package main

import (
	"context"
	"log"
	"os/signal"
	"strings"
	"syscall"

	"ququchat/internal/config"
	database "ququchat/internal/server/db"
	"ququchat/internal/server/storage"
	filesvc "ququchat/internal/service/file"
	taskservice "ququchat/internal/taskservice"
	tasksvc "ququchat/internal/taskservice/task"
	"ququchat/internal/taskservice/task/aigcmq"
	"ququchat/internal/taskservice/task/embeddingmq"
	"ququchat/internal/taskservice/task/llmmq"
	"ququchat/internal/taskservice/task/mcpclient"
	"ququchat/internal/taskservice/task/openaicompat"
)

func main() {
	cfg, err := config.LoadDefault()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	log.Printf("Task Service 启动参数: llm_transport=%s llm_worker=%d embedding_transport=%s embedding_worker=%d aigc_transport=%s aigc_worker=%d",
		cfg.LLM.TransportOrDefault(),
		cfg.LLM.WorkerSizeOrDefault(),
		cfg.Embedding.TransportOrDefault(),
		cfg.Embedding.WorkerSizeOrDefault(),
		cfg.AIGC.TransportOrDefault(),
		cfg.AIGC.WorkerSizeOrDefault(),
	)
	db, err := database.OpenGorm(cfg.Database)
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}
	if sqlDB, err := db.DB(); err != nil {
		log.Fatalf("获取底层连接失败: %v", err)
	} else if err := sqlDB.Ping(); err != nil {
		log.Fatalf("数据库不可用: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}

	var aigcAttachmentSaver aigcmq.AttachmentSaver
	if strings.EqualFold(cfg.AIGC.TransportOrDefault(), "rabbitmq") {
		provider := cfg.Storage.ProviderOrDefault()
		var objStorage storage.ObjectStorage
		var bucket string
		switch provider {
		case "minio":
			objStorage, err = storage.InitMinioStorage(cfg.Minio)
			if err != nil {
				log.Fatalf("初始化 MinIO 失败: %v", err)
			}
			bucket = cfg.Minio.Bucket
		case "oss":
			objStorage, err = storage.InitOSSStorage(cfg.OSS)
			if err != nil {
				log.Fatalf("初始化 OSS 失败: %v", err)
			}
			bucket = cfg.OSS.Bucket
		default:
			log.Fatalf("不支持的对象存储 provider: %s", provider)
		}
		thumb := filesvc.ThumbnailOptions{
			MaxDimension:   cfg.File.Thumbnail.MaxDimensionOrDefault(),
			JPEGQuality:    cfg.File.Thumbnail.JPEGQualityOrDefault(),
			RetryCount:     cfg.File.Thumbnail.RetryCountOrDefault(),
			RetryDelay:     cfg.File.Thumbnail.RetryDelayDuration(),
			MaxSourceBytes: cfg.File.Thumbnail.MaxSourceBytesOrDefault(),
		}
		aigcAttachmentSaver = filesvc.NewService(db, objStorage, bucket, cfg.File.MaxSizeBytes, cfg.File.RetentionDuration(), thumb)
	}

	var llmWorkerPool *llmmq.Pool
	var embeddingWorkerPool *embeddingmq.Pool
	var aigcWorkerPool *aigcmq.Pool

	var ragEmbeddingProvider tasksvc.EmbeddingProvider
	if cfg.Embedding.TransportOrDefault() == "rabbitmq" {
		ragEmbeddingProvider, err = tasksvc.NewMQEmbeddingProvider(tasksvc.MQEmbeddingProviderOptions{
			URL:             cfg.Embedding.RabbitMQURL,
			RequestQueue:    cfg.Embedding.RequestQueueOrDefault(),
			MaxLength:       cfg.Embedding.RequestQueueMaxLengthOrDefault(),
			MessageTTL:      cfg.Embedding.RequestQueueMessageTTLOrDefault(),
			ResponseTimeout: cfg.Embedding.ResponseTimeoutOrDefault(),
		})
		if err != nil {
			log.Fatalf("初始化 RAG Embedding MQ client 失败: %v", err)
		}
		log.Printf("Embedding 请求队列启动成功，queue=%s transport=rabbitmq", cfg.Embedding.RequestQueueOrDefault())
		embeddingProvider, providerErr := openaicompat.NewEmbeddingClient(openaicompat.EmbeddingOptions{
			APIKey:  cfg.Embedding.APIKey,
			BaseURL: cfg.Embedding.BaseURLOrDefault(),
			Model:   cfg.Embedding.ModelOrDefault(),
		})
		if providerErr != nil {
			log.Fatalf("初始化 Embedding MQ worker provider 失败: %v", providerErr)
		}
		embeddingWorkerPool, err = embeddingmq.NewPool(embeddingmq.PoolOptions{
			URL:          cfg.Embedding.RabbitMQURL,
			RequestQueue: cfg.Embedding.RequestQueueOrDefault(),
			MaxLength:    cfg.Embedding.RequestQueueMaxLengthOrDefault(),
			MessageTTL:   cfg.Embedding.RequestQueueMessageTTLOrDefault(),
			Provider:     embeddingProvider,
			Size:         cfg.Embedding.WorkerSizeOrDefault(),
			RPM:          cfg.Embedding.RPMOrDefault(),
			TPM:          cfg.Embedding.TPMOrDefault(),
		})
		if err != nil {
			log.Fatalf("初始化 Embedding worker 线程池失败: %v", err)
		}
		log.Printf("Embedding worker 线程池初始化成功，queue=%s worker=%d transport=rabbitmq", cfg.Embedding.RequestQueueOrDefault(), cfg.Embedding.WorkerSizeOrDefault())
	} else {
		directEmbeddingClient, directErr := openaicompat.NewEmbeddingClient(openaicompat.EmbeddingOptions{
			APIKey:  cfg.Embedding.APIKey,
			BaseURL: cfg.Embedding.BaseURLOrDefault(),
			Model:   cfg.Embedding.ModelOrDefault(),
		})
		if directErr != nil {
			log.Fatalf("初始化 RAG Embedding 直连 client 失败: %v", directErr)
		}
		ragEmbeddingProvider = tasksvc.NewDirectEmbeddingProvider(directEmbeddingClient.Embed)
	}

	var ragVectorStore tasksvc.VectorStore
	if strings.EqualFold(cfg.Vector.ProviderOrDefault(), "qdrant") {
		ragVectorStore, err = tasksvc.NewQdrantVectorStore(tasksvc.QdrantVectorStoreOptions{
			BaseURL:          cfg.Vector.QdrantURLOrDefault(),
			APIKey:           cfg.Vector.APIKey,
			Collection:       cfg.Vector.CollectionOrDefault(),
			Timeout:          cfg.Vector.TimeoutOrDefault(),
			SummaryVectorDim: cfg.Vector.SummaryVectorDimOrDefault(),
		})
		if err != nil {
			log.Fatalf("初始化 Qdrant vector store 失败: %v", err)
		}
	} else {
		log.Fatalf("不支持的向量存储 provider: %s", cfg.Vector.ProviderOrDefault())
	}

	var ragLLMClient tasksvc.LLMClient
	if cfg.LLM.TransportOrDefault() == "rabbitmq" {
		ragLLMClient, err = llmmq.NewClient(llmmq.ClientOptions{
			URL:             cfg.LLM.RabbitMQURL,
			RequestQueue:    cfg.LLM.RequestQueueOrDefault(),
			MaxLength:       cfg.LLM.RequestQueueMaxLengthOrDefault(),
			MessageTTL:      cfg.LLM.RequestQueueMessageTTLOrDefault(),
			ResponseTimeout: cfg.LLM.ResponseTimeoutOrDefault(),
		})
		if err != nil {
			log.Fatalf("初始化 RAG LLM MQ client 失败: %v", err)
		}
		log.Printf("LLM 请求队列启动成功，queue=%s transport=rabbitmq", cfg.LLM.RequestQueueOrDefault())
		llmProvider, providerErr := openaicompat.NewLLMClient(openaicompat.LLMOptions{
			APIKey:  cfg.LLM.APIKey,
			BaseURL: cfg.LLM.BaseURLOrDefault(),
			Model:   cfg.LLM.ModelOrDefault(),
		})
		if providerErr != nil {
			log.Fatalf("初始化 LLM MQ worker provider 失败: %v", providerErr)
		}
		llmWorkerPool, err = llmmq.NewPool(llmmq.PoolOptions{
			URL:          cfg.LLM.RabbitMQURL,
			RequestQueue: cfg.LLM.RequestQueueOrDefault(),
			MaxLength:    cfg.LLM.RequestQueueMaxLengthOrDefault(),
			MessageTTL:   cfg.LLM.RequestQueueMessageTTLOrDefault(),
			Provider:     llmProvider,
			Size:         cfg.LLM.WorkerSizeOrDefault(),
			RPM:          cfg.LLM.RPMOrDefault(),
			TPM:          cfg.LLM.TPMOrDefault(),
		})
		if err != nil {
			log.Fatalf("初始化 LLM worker 线程池失败: %v", err)
		}
		log.Printf("LLM worker 线程池初始化成功，queue=%s worker=%d transport=rabbitmq", cfg.LLM.RequestQueueOrDefault(), cfg.LLM.WorkerSizeOrDefault())
	} else {
		directLLMClient, directErr := openaicompat.NewLLMClient(openaicompat.LLMOptions{
			APIKey:  cfg.LLM.APIKey,
			BaseURL: cfg.LLM.BaseURLOrDefault(),
			Model:   cfg.LLM.ModelOrDefault(),
		})
		if directErr != nil {
			log.Fatalf("初始化 RAG LLM 直连 client 失败: %v", directErr)
		}
		ragLLMClient = directLLMClient
	}
	if strings.EqualFold(cfg.AIGC.TransportOrDefault(), "rabbitmq") {
		aigcProvider, providerErr := openaicompat.NewAIGCClient(openaicompat.AIGCOptions{
			APIKey:  cfg.AIGC.APIKey,
			BaseURL: cfg.AIGC.BaseURLOrDefault(),
			Model:   cfg.AIGC.ModelOrDefault(),
			Timeout: cfg.AIGC.ResponseTimeoutOrDefault(),
		})
		if providerErr != nil {
			log.Fatalf("初始化 AIGC MQ worker provider 失败: %v", providerErr)
		}
		aigcWorkerPool, err = aigcmq.NewPool(aigcmq.PoolOptions{
			URL:             cfg.AIGC.RabbitMQURL,
			RequestQueue:    cfg.AIGC.RequestQueueOrDefault(),
			MaxLength:       cfg.AIGC.RequestQueueMaxLengthOrDefault(),
			MessageTTL:      cfg.AIGC.RequestQueueMessageTTLOrDefault(),
			Provider:        aigcProvider,
			AttachmentSaver: aigcAttachmentSaver,
			Size:            cfg.AIGC.WorkerSizeOrDefault(),
			IPM:             cfg.AIGC.IPMOrDefault(),
			IPD:             cfg.AIGC.IPDOrDefault(),
		})
		if err != nil {
			log.Fatalf("初始化 AIGC worker 线程池失败: %v", err)
		}
		log.Printf("AIGC worker 线程池初始化成功，queue=%s worker=%d transport=rabbitmq", cfg.AIGC.RequestQueueOrDefault(), cfg.AIGC.WorkerSizeOrDefault())
	}
	if strings.EqualFold(cfg.Task.QueueTransportOrDefault(), "rabbitmq") {
		if err := tasksvc.MigrateRabbitMQQueue(tasksvc.RabbitMQQueueOptions{
			URL:          cfg.Task.QueueRabbitMQURL,
			QueueName:    cfg.Task.QueueRabbitMQNameOrDefault(),
			ExchangeName: cfg.Task.QueueRabbitMQExchangeOrDefault(),
			MaxPriority:  cfg.Task.QueueRabbitMQMaxPriorityOrDefault(),
			MaxLength:    cfg.Task.QueueRabbitMQMaxLengthOrDefault(),
		}); err != nil {
			log.Fatalf("迁移任务队列失败: %v", err)
		}
		log.Printf("任务队列迁移完成，queue=%s exchange=%s", cfg.Task.QueueRabbitMQNameOrDefault(), cfg.Task.QueueRabbitMQExchangeOrDefault())
	}
	var mcpMultiClient *mcpclient.MultiClient
	if len(cfg.MCPServers) > 0 {
		serverCfg := make(map[string]mcpclient.ServerConfig, len(cfg.MCPServers))
		for name, item := range cfg.MCPServers {
			serverCfg[name] = mcpclient.ServerConfig{
				Endpoint:  item.Endpoint,
				APIKey:    item.APIKey,
				Headers:   item.Headers,
				Name:      item.Name,
				Version:   item.Version,
				TimeoutMs: item.TimeoutMs,
			}
		}
		client, clientErr := mcpclient.NewMultiClientFromServers(context.Background(), serverCfg)
		if clientErr != nil {
			log.Printf("初始化 MCP MultiClient 失败，将跳过 MCP: %v", clientErr)
		} else {
			mcpMultiClient = client
			defer mcpMultiClient.Close()
		}
	}

	taskService := taskservice.NewService(db, tasksvc.RuntimeOptions{
		QueueHighCap:                     cfg.Task.QueueHighCapOrDefault(),
		QueueNormalCap:                   cfg.Task.QueueNormalCapOrDefault(),
		QueueLowCap:                      cfg.Task.QueueLowCapOrDefault(),
		QueueTransport:                   cfg.Task.QueueTransportOrDefault(),
		QueueRabbitMQURL:                 cfg.Task.QueueRabbitMQURL,
		QueueRabbitMQName:                cfg.Task.QueueRabbitMQNameOrDefault(),
		QueueRabbitMQExchange:            cfg.Task.QueueRabbitMQExchangeOrDefault(),
		QueueRabbitMQMaxPriority:         cfg.Task.QueueRabbitMQMaxPriorityOrDefault(),
		QueueRabbitMQMaxLength:           cfg.Task.QueueRabbitMQMaxLengthOrDefault(),
		DoneEventRabbitMQURL:             cfg.Task.DoneEventMQURLOrDefault(),
		DoneEventQueueName:               cfg.Task.DoneEventQueueOrDefault(),
		DoneEventQueueMaxLength:          cfg.Task.DoneEventQueueMaxLengthOrDefault(),
		DoneEventQueueMessageTTL:         cfg.Task.DoneEventQueueMessageTTLOrDefault(),
		DoneEventPublishRetryMaxAttempts: cfg.Task.DonePublishRetryMaxAttemptsOrDefault(),
		DoneEventPublishRetryDelay:       cfg.Task.DonePublishRetryDelayOrDefault(),
		DoneEventConsumeRetryMaxAttempts: cfg.Task.DoneConsumeRetryMaxAttemptsOrDefault(),
		DoneEventConsumeRetryDelay:       cfg.Task.DoneConsumeRetryDelayOrDefault(),
		InputRetryMaxAttempts:            cfg.Task.InputRetryMaxAttemptsOrDefault(),
		InputRetryDelay:                  cfg.Task.InputRetryDelayOrDefault(),
		WorkerSize:                       cfg.Task.WorkerSizeOrDefault(),
		LLMTransport:                     cfg.LLM.TransportOrDefault(),
		LLMMQURL:                         cfg.LLM.RabbitMQURL,
		LLMMQQueue:                       cfg.LLM.RequestQueueOrDefault(),
		LLMMQMaxLength:                   cfg.LLM.RequestQueueMaxLengthOrDefault(),
		LLMMQMessageTTL:                  cfg.LLM.RequestQueueMessageTTLOrDefault(),
		LLMMQTimeout:                     cfg.LLM.ResponseTimeoutOrDefault(),
		LLMClient:                        ragLLMClient,
		LLMAPIKey:                        cfg.LLM.APIKey,
		LLMBaseURL:                       cfg.LLM.BaseURLOrDefault(),
		LLMModel:                         cfg.LLM.ModelOrDefault(),
		AIGCTransport:                    cfg.AIGC.TransportOrDefault(),
		AIGCMQURL:                        cfg.AIGC.RabbitMQURL,
		AIGCMQQueue:                      cfg.AIGC.RequestQueueOrDefault(),
		AIGCMQMaxLength:                  cfg.AIGC.RequestQueueMaxLengthOrDefault(),
		AIGCMQMessageTTL:                 cfg.AIGC.RequestQueueMessageTTLOrDefault(),
		AIGCMQTimeout:                    cfg.AIGC.ResponseTimeoutOrDefault(),
		EmbeddingProvider:                ragEmbeddingProvider,
		VectorStore:                      ragVectorStore,
		EmbeddingModelRaw:                cfg.Embedding.ModelOrDefault(),
		EmbeddingModelSummary:            cfg.Embedding.ModelOrDefault(),
		SummaryVectorDim:                 cfg.Vector.SummaryVectorDimOrDefault(),
		RAGStopPhrases:                   cfg.RAGStopPhrases.StopPhrases,
		MCPMultiClient:                   mcpMultiClient,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if embeddingWorkerPool != nil {
		if err := embeddingWorkerPool.Start(ctx); err != nil {
			log.Fatalf("启动 Embedding worker 线程池失败: %v", err)
		}
		log.Printf("Embedding worker 线程池已启动，queue=%s worker=%d", cfg.Embedding.RequestQueueOrDefault(), cfg.Embedding.WorkerSizeOrDefault())
	}
	if llmWorkerPool != nil {
		if err := llmWorkerPool.Start(ctx); err != nil {
			log.Fatalf("启动 LLM worker 线程池失败: %v", err)
		}
		log.Printf("LLM worker 线程池已启动，queue=%s worker=%d", cfg.LLM.RequestQueueOrDefault(), cfg.LLM.WorkerSizeOrDefault())
	}
	if aigcWorkerPool != nil {
		if err := aigcWorkerPool.Start(ctx); err != nil {
			log.Fatalf("启动 AIGC worker 线程池失败: %v", err)
		}
		log.Printf("AIGC worker 线程池已启动，queue=%s worker=%d", cfg.AIGC.RequestQueueOrDefault(), cfg.AIGC.WorkerSizeOrDefault())
	}
	dlqURLs := make([]string, 0, 4)
	if strings.EqualFold(cfg.Task.QueueTransportOrDefault(), "rabbitmq") {
		dlqURLs = append(dlqURLs, cfg.Task.QueueRabbitMQURL)
	}
	if strings.EqualFold(cfg.LLM.TransportOrDefault(), "rabbitmq") {
		dlqURLs = append(dlqURLs, cfg.LLM.RabbitMQURL)
	}
	if strings.EqualFold(cfg.AIGC.TransportOrDefault(), "rabbitmq") {
		dlqURLs = append(dlqURLs, cfg.AIGC.RabbitMQURL)
	}
	if strings.EqualFold(cfg.Embedding.TransportOrDefault(), "rabbitmq") {
		dlqURLs = append(dlqURLs, cfg.Embedding.RabbitMQURL)
	}
	startedDLQ := map[string]struct{}{}
	for _, rawURL := range dlqURLs {
		url := strings.TrimSpace(rawURL)
		if url == "" {
			continue
		}
		if _, exists := startedDLQ[url]; exists {
			continue
		}
		dlqConsumer, err := taskservice.NewRabbitMQTaskDeadLetterConsumer(taskservice.RabbitMQTaskDeadLetterConsumerOptions{
			URL: url,
			DB:  db,
		})
		if err != nil {
			log.Fatalf("初始化 Task DLQ consumer 失败 url=%s err=%v", url, err)
		}
		startedDLQ[url] = struct{}{}
		consumer := dlqConsumer
		go func(targetURL string) {
			if runErr := consumer.Start(ctx); runErr != nil {
				log.Printf("Task DLQ consumer 退出 url=%s err=%v", targetURL, runErr)
			}
		}(url)
	}
	taskService.Start(ctx)
	log.Printf("独立 Task Service 已启动，queue=%s exchange=%s worker=%d", cfg.Task.QueueRabbitMQNameOrDefault(), cfg.Task.QueueRabbitMQExchangeOrDefault(), cfg.Task.WorkerSizeOrDefault())
	<-ctx.Done()
	log.Printf("独立 Task Service 正在退出")
}
