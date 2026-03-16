package app

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"ququchat/agent/internal/config"
	"ququchat/agent/internal/core/executor"
	"ququchat/agent/internal/core/scheduler"
	"ququchat/agent/internal/core/task"
	"ququchat/agent/internal/core/worker"
	kafkamq "ququchat/agent/internal/mq/kafka"
	"ququchat/agent/internal/queue"
	llmservice "ququchat/agent/internal/service/llm"
	"ququchat/agent/internal/store"
	"ququchat/agent/pkg/llmmsg"
)

func StartLLMRuntime(ctx context.Context, settings config.Settings) (func() error, error) {
	memStore := store.NewMemoryStore()
	memQueue := queue.NewMemoryPriorityQueue(settings.QueueHighCap, settings.QueueNormalCap, settings.QueueLowCap)
	dispatcher := scheduler.NewDispatcher(memQueue)
	llmSvc := llmservice.NewService(memStore, dispatcher)
	exec := executor.NewDefaultExecutor()
	resultProducer := kafkamq.NewResultProducer(settings.KafkaBrokers, settings.KafkaResult)

	pool := worker.NewPool(memQueue, memStore, exec, settings.WorkerSize, func(cbCtx context.Context, doneTask *task.Task) {
		if doneTask == nil {
			return
		}
		result := &llmmsg.Result{
			TaskID:     doneTask.ID,
			RequestID:  doneTask.RequestID,
			TaskType:   string(doneTask.Type),
			Status:     string(doneTask.Status),
			Error:      doneTask.ErrorMessage,
			StartedAt:  doneTask.CreatedAt.UnixMilli(),
			FinishedAt: time.Now().UnixMilli(),
		}
		if doneTask.Result.Text != nil {
			result.OutputText = *doneTask.Result.Text
			output, err := json.Marshal(map[string]string{
				"text": result.OutputText,
			})
			if err == nil {
				result.Output = output
			}
		}
		if err := resultProducer.Publish(cbCtx, result); err != nil {
			log.Printf("[llm-runtime] publish result failed task=%s err=%v", doneTask.ID, err)
		}
	})
	go pool.Start(ctx)

	consumer := kafkamq.NewIngressConsumer(settings.KafkaBrokers, settings.KafkaIngress, settings.KafkaGroupID)
	go func() {
		if err := consumer.Start(ctx, func(handleCtx context.Context, msg *llmmsg.Ingress) error {
			_, err := llmSvc.SubmitIngress(msg)
			return err
		}); err != nil {
			log.Printf("[llm-runtime] ingress consumer stopped err=%v", err)
		}
	}()

	return func() error {
		if err := consumer.Close(); err != nil {
			return err
		}
		if err := resultProducer.Close(); err != nil {
			return err
		}
		return nil
	}, nil
}
