package main

import (
	"context"
	"fmt"
	"log"
	"time"
)

type Scheduler struct {
	store  *Store
	vk     *VKClient
	config *Config
}

func NewScheduler(store *Store, vk *VKClient, cfg *Config) *Scheduler {
	return &Scheduler{store: store, vk: vk, config: cfg}
}

func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// При старте — попытаться досоздать пропущенное и обработать pending
	s.recoverPending(ctx)
	s.checkAndCreate(ctx)

	for {
		select {
		case <-ticker.C:
			s.checkAndCreate(ctx)
			s.recoverPending(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (s *Scheduler) checkAndCreate(ctx context.Context) {
	now := time.Now().In(s.config.Timezone)

	// Проверяем сегодня и завтра (на случай, если окно создания на завтра уже наступило)
	for _, d := range []time.Time{now, now.AddDate(0, 0, 1)} {
		dateStr := d.Format("2006-01-02")
		if s.shouldCreatePoll(dateStr, d) {
			s.createPollForDate(ctx, dateStr, d)
		}
	}
}

func (s *Scheduler) shouldCreatePoll(dateStr string, d time.Time) bool {
	s.store.Lock()
	defer s.store.Unlock()

	// Уже есть active или pending — не дублируем
	if _, ok := s.store.State.ActivePolls[dateStr]; ok {
		return false
	}
	if _, ok := s.store.State.PendingPolls[dateStr]; ok {
		return false
	}

	slot := resolveSlot(s.store.State, s.store.Schedule, dateStr, d)
	if slot == nil {
		return false
	}

	// Окно создания: 09:00 дня занятия (можно изменить)
	createTime := time.Date(d.Year(), d.Month(), d.Day(), 9, 0, 0, 0, s.config.Timezone)
	if time.Now().In(s.config.Timezone).Before(createTime) {
		return false
	}

	// Если занятие уже прошло более чем на час — не создаём
	classTime, err := time.ParseInLocation("15:04", slot.Time, s.config.Timezone)
	if err != nil {
		return false
	}
	classDateTime := time.Date(d.Year(), d.Month(), d.Day(), classTime.Hour(), classTime.Minute(), 0, 0, s.config.Timezone)
	if time.Now().In(s.config.Timezone).After(classDateTime.Add(1 * time.Hour)) {
		return false
	}

	return true
}

// resolveSlot возвращает слот с учётом overrides, либо nil если занятий нет.
func resolveSlot(state *State, schedule []ScheduleSlot, dateStr string, d time.Time) *ScheduleSlot {
	// 1. Overrides приоритетнее шаблона.
	if ov, ok := state.Overrides[dateStr]; ok {
		if ov.Status == "cancelled" {
			return nil
		}

		timeStr := ov.NewTime
		if timeStr == "" {
			// На случай "extra" без NewTime или кривых данных — fallback на оригинал.
			timeStr = ov.OriginalTime
		}
		if timeStr == "" || ov.Type == "" {
			return nil
		}
		return &ScheduleSlot{
			DayOfWeek: d.Weekday(),
			Type:      ov.Type,
			Time:      timeStr,
			Title:     typeTitle(ov.Type),
		}
	}

	// 2. Иначе ищем регулярный слот по дню недели.
	for _, slot := range schedule {
		if slot.DayOfWeek == d.Weekday() {
			cp := slot // копия, чтобы не отдавать указатель на элемент слайса
			return &cp
		}
	}
	return nil
}

func (s *Scheduler) createPollForDate(ctx context.Context, dateStr string, d time.Time) {
	s.store.Lock()
	slot := resolveSlot(s.store.State, s.store.Schedule, dateStr, d)
	if slot == nil {
		s.store.Unlock()
		return
	}

	// Журнал намерений: pending
	pending := &PendingPoll{
		Date:      dateStr,
		Type:      slot.Type,
		Time:      slot.Time,
		ChatID:    s.config.ChatID,
		Attempts:  0,
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	s.store.State.PendingPolls[dateStr] = pending
	_ = s.store.SaveState()
	s.store.Unlock()

	s.processPending(ctx, pending)
}

// processPending выполняет создание опроса и отправку, безопасно для повторного вызова.
func (s *Scheduler) processPending(ctx context.Context, p *PendingPoll) {
	question := fmt.Sprintf("%s — %s, %s. Кто идёт?", typeTitle(p.Type), formatDateHuman(p.Date), p.Time)
	answers := []string{"✅ Иду", "❌ Не иду", "🤔 Не уверен"}

	// Фаза 1: создание опроса (если ещё не создан)
	if p.PollID == 0 {
		var pollID int
		var answerIDs []int
		err := retry(ctx, 3, func() error {
			var e error
			pollID, answerIDs, e = s.vk.CreatePoll(ctx, -s.config.GroupID, question, answers, endDateFor(p.Date, s.config.Timezone))
			return e
		})
		if err != nil {
			s.handleFatal(ctx, p, err, "создание опроса")
			return
		}

		s.store.Lock()
		p.PollID = pollID
		// Сохраняем answerIDs сразу в pending? Лучше в ActivePoll после успешной отправки.
		// Но для /who они понадобятся. Можно сохранить в pending, но структура pending этого не предусматривает.
		// Добавим поле AnswerIDs в PendingPoll? Или создадим временную запись.
		// Упростим: сохраним answerIDs в pending через отдельное поле (добавим в модель).
		// В models.go PendingPoll добавьте: AnswerIDs []int `json:"answer_ids,omitempty"`
		p.AnswerIDs = answerIDs
		_ = s.store.SaveState()
		s.store.Unlock()
	}

	// Фаза 2: отправка сообщения
	randomID := randomIDFor(p.Date, p.Attempts)
	var msgID int
	err := retry(ctx, 5, func() error {
		var e error
		msgID, e = s.vk.SendPoll(ctx, p.ChatID, -s.config.GroupID, p.PollID, "", randomID)
		return e
	})
	if err != nil {
		s.store.Lock()
		p.Attempts++
		_ = s.store.SaveState()
		s.store.Unlock()
		log.Printf("poll %d created but send failed (attempt %d): %v", p.PollID, p.Attempts, err)
		return
	}

	// Успех: pending → active
	s.store.Lock()
	delete(s.store.State.PendingPolls, p.Date)
	s.store.State.ActivePolls[p.Date] = &ActivePoll{
		PollID:    p.PollID,
		OwnerID:   -s.config.GroupID,
		MessageID: msgID,
		ChatID:    p.ChatID,
		AnswerIDs: p.AnswerIDs,
	}
	_ = s.store.SaveState()
	s.store.Unlock()
	log.Printf("poll created and sent for %s: poll_id=%d, message_id=%d", p.Date, p.PollID, msgID)
}

func (s *Scheduler) handleFatal(ctx context.Context, p *PendingPoll, err error, stage string) {
	if isFatalPermission(err) {
		log.Printf("fatal permission error during %s: %v", stage, err)
		s.vk.SendText(ctx, p.ChatID, "⚠️ Не могу создать опрос: проверьте права бота на создание опросов в сообществе.", randomIDFor(p.Date, 0))
		// Удаляем pending, чтобы не зацикливаться
		s.store.Lock()
		delete(s.store.State.PendingPolls, p.Date)
		_ = s.store.SaveState()
		s.store.Unlock()
	} else {
		log.Printf("non-retryable error during %s: %v", stage, err)
		// Оставляем pending для ручного разбора или следующей попытки
	}
}

// recoverPending при старте пытается завершить «подвисшие» pending.
func (s *Scheduler) recoverPending(ctx context.Context) {
	s.store.Lock()
	pendings := make([]*PendingPoll, 0, len(s.store.State.PendingPolls))
	for _, p := range s.store.State.PendingPolls {
		pendings = append(pendings, p)
	}
	s.store.Unlock()

	for _, p := range pendings {
		// Если pending висит слишком долго — логируем
		created, _ := time.Parse(time.RFC3339, p.CreatedAt)
		if time.Since(created) > 2*time.Hour && p.Attempts > 10 {
			log.Printf("pending poll for %s stuck for too long, skipping", p.Date)
			continue
		}
		s.processPending(ctx, p)
	}
}
