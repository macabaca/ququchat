package main

import (
	"context"
	"log"
	"os/signal"
	"strings"
	"syscall"

	"ququchat/internal/config"
	database "ququchat/internal/server/db"
	taskservice "ququchat/internal/taskservice"
	tasksvc "ququchat/internal/taskservice/task"
	"ququchat/internal/taskservice/task/llmmq"
	"ququchat/internal/taskservice/task/openaicompat"
)

func main() {
	cfg, err := config.LoadDefault()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
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

	var ragEmbeddingProvider tasksvc.EmbeddingProvider
	if cfg.Embedding.TransportOrDefault() == "rabbitmq" {
		ragEmbeddingProvider, err = tasksvc.NewMQEmbeddingProvider(tasksvc.MQEmbeddingProviderOptions{
			URL:             cfg.Embedding.RabbitMQURL,
			RequestQueue:    cfg.Embedding.RequestQueueOrDefault(),
			ResponseTimeout: cfg.Embedding.ResponseTimeoutOrDefault(),
		})
		if err != nil {
			log.Fatalf("初始化 RAG Embedding MQ client 失败: %v", err)
		}
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
			ResponseTimeout: cfg.LLM.ResponseTimeoutOrDefault(),
		})
		if err != nil {
			log.Fatalf("初始化 RAG LLM MQ client 失败: %v", err)
		}
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

	taskService := taskservice.NewService(db, tasksvc.RuntimeOptions{
		QueueHighCap:                     cfg.Task.QueueHighCapOrDefault(),
		QueueNormalCap:                   cfg.Task.QueueNormalCapOrDefault(),
		QueueLowCap:                      cfg.Task.QueueLowCapOrDefault(),
		QueueTransport:                   cfg.Task.QueueTransportOrDefault(),
		QueueRabbitMQURL:                 cfg.Task.QueueRabbitMQURL,
		QueueRabbitMQName:                cfg.Task.QueueRabbitMQNameOrDefault(),
		QueueRabbitMQExchange:            cfg.Task.QueueRabbitMQExchangeOrDefault(),
		QueueRabbitMQMaxPriority:         cfg.Task.QueueRabbitMQMaxPriorityOrDefault(),
		DoneEventRabbitMQURL:             cfg.Task.DoneEventMQURLOrDefault(),
		DoneEventQueueName:               cfg.Task.DoneEventQueueOrDefault(),
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
		LLMMQTimeout:                     cfg.LLM.ResponseTimeoutOrDefault(),
		LLMClient:                        ragLLMClient,
		LLMAPIKey:                        cfg.LLM.APIKey,
		LLMBaseURL:                       cfg.LLM.BaseURLOrDefault(),
		LLMModel:                         cfg.LLM.ModelOrDefault(),
		AIGCTransport:                    cfg.AIGC.TransportOrDefault(),
		AIGCMQURL:                        cfg.AIGC.RabbitMQURL,
		AIGCMQQueue:                      cfg.AIGC.RequestQueueOrDefault(),
		AIGCMQTimeout:                    cfg.AIGC.ResponseTimeoutOrDefault(),
		EmbeddingProvider:                ragEmbeddingProvider,
		VectorStore:                      ragVectorStore,
		EmbeddingModelRaw:                cfg.Embedding.ModelOrDefault(),
		EmbeddingModelSummary:            cfg.Embedding.ModelOrDefault(),
		SummaryVectorDim:                 cfg.Vector.SummaryVectorDimOrDefault(),
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	taskService.Start(ctx)
	log.Printf("独立 Task Service 已启动，queue=%s exchange=%s worker=%d", cfg.Task.QueueRabbitMQNameOrDefault(), cfg.Task.QueueRabbitMQExchangeOrDefault(), cfg.Task.WorkerSizeOrDefault())
	<-ctx.Done()
	log.Printf("独立 Task Service 正在退出")
}
