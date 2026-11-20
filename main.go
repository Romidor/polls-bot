package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/SevereCloud/vksdk/v3/events"
	longpoll "github.com/SevereCloud/vksdk/v3/longpoll-bot"
)

func main() {
	cfg := LoadConfig()

	store, err := NewStore(cfg.DataDir)
	if err != nil {
		log.Fatalf("failed to init store: %v", err)
	}

	vk := NewVKClient(cfg.Token)

	// Планировщик
	scheduler := NewScheduler(store, vk, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go scheduler.Run(ctx)

	// Long Poll
	lp, err := longpoll.NewLongPoll(vk.VK, cfg.GroupID)
	if err != nil {
		log.Fatalf("failed to init longpoll: %v", err)
	}

	// Восстанавливаем ts из state, если есть
	store.Lock()
	if store.State.LongPollTs != "" {
		lp.Ts = store.State.LongPollTs
		log.Printf("restored longpoll ts: %s", lp.Ts)
	}
	store.Unlock()

	// Сохраняем ts после каждого полного ответа
	lp.FullResponse(func(resp longpoll.Response) {
		store.Lock()
		store.State.LongPollTs = resp.Ts
		_ = store.SaveState()
		store.Unlock()
	})

	handler := NewCommandHandler(store, vk, cfg)

	lp.MessageNew(func(ctx context.Context, obj events.MessageNewObject) {
		// Игнорируем сообщения от самого бота и не из целевого чата
		if obj.Message.FromID < 0 {
			return
		}
		if obj.Message.PeerID != cfg.ChatID {
			return
		}
		handler.Handle(ctx, obj)
	})

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("shutting down...")
		cancel()
		lp.Shutdown()
	}()

	log.Println("bot started")
	if err := lp.Run(); err != nil {
		log.Fatalf("longpoll error: %v", err)
	}
}
