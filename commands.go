package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/SevereCloud/vksdk/v3/events"
)

type CommandHandler struct {
	store  *Store
	vk     *VKClient
	config *Config
}

func NewCommandHandler(store *Store, vk *VKClient, cfg *Config) *CommandHandler {
	return &CommandHandler{store: store, vk: vk, config: cfg}
}

func (h *CommandHandler) Handle(ctx context.Context, obj events.MessageNewObject) {
	text := strings.TrimSpace(obj.Message.Text)
	if !strings.HasPrefix(text, "/") {
		return
	}

	parts := strings.Fields(text)
	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	// /today и /week доступны всем
	switch cmd {
	case "/today":
		h.handleToday(ctx, obj)
		return
	case "/week":
		h.handleWeek(ctx, obj)
		return
	case "/who":
		h.handleWho(ctx, obj, args)
		return
	}

	// Остальные команды — только админам
	if !h.config.AdminIDs[obj.Message.FromID] {
		h.vk.SendText(ctx, obj.Message.PeerID, "⛔ Только для администратора.", 0)
		return
	}

	switch cmd {
	case "/schedule":
		h.handleSchedule(ctx, obj, args)
	case "/plan":
		h.handlePlan(ctx, obj, args)
	case "/move":
		h.handleMove(ctx, obj, args)
	case "/cancel":
		h.handleCancel(ctx, obj, args)
	default:
		h.vk.SendText(ctx, obj.Message.PeerID, "Неизвестная команда. Используйте /schedule help.", 0)
	}
}

// ---------- /schedule ----------

func (h *CommandHandler) handleSchedule(ctx context.Context, obj events.MessageNewObject, args []string) {
	sub := "list"
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}

	switch sub {
	case "list":
		h.scheduleList(ctx, obj)
	case "add":
		h.scheduleAdd(ctx, obj, args[1:])
	case "remove":
		h.scheduleRemove(ctx, obj, args[1:])
	case "clear":
		if len(args) > 1 && args[1] == "confirm" {
			h.scheduleClear(ctx, obj)
		} else {
			h.vk.SendText(ctx, obj.Message.PeerID, "❓ Очистить всё расписание? Отправьте `/schedule clear confirm` в течение 60 секунд.", 0)
		}
	case "help":
		h.scheduleHelp(ctx, obj)
	default:
		h.vk.SendText(ctx, obj.Message.PeerID, "Подкоманды: list, add, remove, clear, help", 0)
	}
}

func (h *CommandHandler) scheduleList(ctx context.Context, obj events.MessageNewObject) {
	h.store.Lock()
	defer h.store.Unlock()

	if len(h.store.Schedule) == 0 {
		h.vk.SendText(ctx, obj.Message.PeerID, "📅 Расписание пусто. Добавьте слот командой `/schedule add`.", 0)
		return
	}

	var sb strings.Builder
	sb.WriteString("📅 Расписание:\n")
	dayNames := map[time.Weekday]string{
		time.Monday: "Пн", time.Tuesday: "Вт", time.Wednesday: "Ср",
		time.Thursday: "Чт", time.Friday: "Пт", time.Saturday: "Сб", time.Sunday: "Вс",
	}
	for _, s := range h.store.Schedule {
		sb.WriteString(fmt.Sprintf("%s — %s %s, %s\n", dayNames[s.DayOfWeek], typeEmoji(s.Type), typeTitle(s.Type), s.Time))
	}
	h.vk.SendText(ctx, obj.Message.PeerID, sb.String(), 0)
}

func (h *CommandHandler) scheduleAdd(ctx context.Context, obj events.MessageNewObject, args []string) {
	if len(args) < 3 {
		h.vk.SendText(ctx, obj.Message.PeerID, "Использование: `/schedule add <день> <тип> <время>`\nПример: `/schedule add ср зал 19:00`", 0)
		return
	}

	day, ok := parseDay(args[0])
	if !ok {
		h.vk.SendText(ctx, obj.Message.PeerID, "Неизвестный день. Используйте: пн, вт, ср, чт, пт, сб, вс (или monday…).", 0)
		return
	}
	typ, ok := parseType(args[1])
	if !ok {
		h.vk.SendText(ctx, obj.Message.PeerID, "Неизвестный тип. Используйте: бассейн, зал, остров, лекция.", 0)
		return
	}
	timeStr, ok := parseTime(args[2])
	if !ok {
		h.vk.SendText(ctx, obj.Message.PeerID, "Неверный формат времени. Используйте HH:MM, например 19:00.", 0)
		return
	}

	h.store.Lock()
	replaced := false
	for i, s := range h.store.Schedule {
		if s.DayOfWeek == day {
			replaced = true
			h.store.Schedule[i] = ScheduleSlot{DayOfWeek: day, Type: typ, Time: timeStr, Title: typeTitle(typ), ChatId: obj.Message.PeerID}
			break
		}
	}
	if !replaced {
		h.store.Schedule = append(h.store.Schedule, ScheduleSlot{DayOfWeek: day, Type: typ, Time: timeStr, Title: typeTitle(typ), ChatId: obj.Message.PeerID})
	}
	_ = h.store.SaveSchedule()
	h.store.Unlock()

	h.store.LogScheduleChange("add", fmt.Sprintf("%v %s %s", day, typ, timeStr), strconv.Itoa(obj.Message.FromID))

	if replaced {
		h.vk.SendText(ctx, obj.Message.PeerID, fmt.Sprintf("✅ Обновлено: %s — %s %s (было изменено).", dayName(day), typeTitle(typ), timeStr), 0)
	} else {
		h.vk.SendText(ctx, obj.Message.PeerID, fmt.Sprintf("✅ Добавлено: %s — %s %s.", dayName(day), typeTitle(typ), timeStr), 0)
	}
}

func (h *CommandHandler) scheduleRemove(ctx context.Context, obj events.MessageNewObject, args []string) {
	if len(args) < 1 {
		h.vk.SendText(ctx, obj.Message.PeerID, "Использование: `/schedule remove <день>`", 0)
		return
	}
	day, ok := parseDay(args[0])
	if !ok {
		h.vk.SendText(ctx, obj.Message.PeerID, "Неизвестный день.", 0)
		return
	}

	h.store.Lock()
	var removed *ScheduleSlot
	for i, s := range h.store.Schedule {
		if s.DayOfWeek == day && s.ChatId == obj.Message.PeerID {
			removed = &s
			h.store.Schedule = append(h.store.Schedule[:i], h.store.Schedule[i+1:]...)
			break
		}
	}
	_ = h.store.SaveSchedule()
	h.store.Unlock()

	if removed == nil {
		h.vk.SendText(ctx, obj.Message.PeerID, "На этот день ничего не запланировано.", 0)
		return
	}

	h.store.LogScheduleChange("remove", fmt.Sprintf("%v", day), strconv.Itoa(obj.Message.FromID))
	h.vk.SendText(ctx, obj.Message.PeerID, fmt.Sprintf("✅ Удалено: %s — %s %s.", dayName(day), typeTitle(removed.Type), removed.Time), 0)
}

func (h *CommandHandler) scheduleClear(ctx context.Context, obj events.MessageNewObject) {
	h.store.Lock()
	h.store.Schedule = []ScheduleSlot{}
	_ = h.store.SaveSchedule()
	h.store.Unlock()
	h.store.LogScheduleChange("clear", "all", strconv.Itoa(obj.Message.FromID))
	h.vk.SendText(ctx, obj.Message.PeerID, "✅ Расписание очищено. Overrides сохранены — они продолжат действовать.", 0)
}

func (h *CommandHandler) scheduleHelp(ctx context.Context, obj events.MessageNewObject) {
	text := `📅 Команды расписания:
/schedule list — показать расписание
/schedule add <день> <тип> <время> — добавить/заменить слот
/schedule remove <день> — удалить слот
/schedule clear — очистить всё (с подтверждением)
/schedule help — эта справка

Пример: /schedule add ср зал 19:00`
	h.vk.SendText(ctx, obj.Message.PeerID, text, 0)
}

// ---------- /plan, /move, /cancel ----------

func (h *CommandHandler) handlePlan(ctx context.Context, obj events.MessageNewObject, args []string) {
	if len(args) < 3 {
		h.vk.SendText(ctx, obj.Message.PeerID, "Использование: `/plan <дата> <тип> <время>`\nПример: `/plan 25.11 бассейн 10:00` или `/plan завтра зал 18:00`", 0)
		return
	}
	dateStr, ok := parseDate(args[0], h.config.Timezone)
	if !ok {
		h.vk.SendText(ctx, obj.Message.PeerID, "Неверная дата. Используйте ДД.ММ, ГГГГ-ММ-ДД, «сегодня» или «завтра».", 0)
		return
	}
	typ, ok := parseType(args[1])
	if !ok {
		h.vk.SendText(ctx, obj.Message.PeerID, "Неизвестный тип. Используйте: бассейн, зал, остров, лекция.", 0)
		return
	}
	timeStr, ok := parseTime(args[2])
	if !ok {
		h.vk.SendText(ctx, obj.Message.PeerID, "Неверный формат времени.", 0)
		return
	}

	h.store.Lock()
	h.store.State.Overrides[dateStr] = &Override{
		Status:  "extra",
		Type:    typ,
		NewTime: timeStr,
		ChatId:  obj.Message.PeerID,
	}
	_ = h.store.SaveState()
	h.store.Unlock()

	h.vk.SendText(ctx, obj.Message.PeerID, fmt.Sprintf("✅ Запланировано дополнительное занятие: %s — %s %s.", dateStr, typeTitle(typ), timeStr), 0)
}

func (h *CommandHandler) handleMove(ctx context.Context, obj events.MessageNewObject, args []string) {
	if len(args) < 2 {
		h.vk.SendText(ctx, obj.Message.PeerID, "Использование: `/move <дата> <новое_время>`\nПример: `/move 21.11 20:30`", 0)
		return
	}
	dateStr, ok := parseDate(args[0], h.config.Timezone)
	if !ok {
		h.vk.SendText(ctx, obj.Message.PeerID, "Неверная дата.", 0)
		return
	}
	newTime, ok := parseTime(args[1])
	if !ok {
		h.vk.SendText(ctx, obj.Message.PeerID, "Неверный формат времени.", 0)
		return
	}

	h.store.Lock()
	// Определяем текущий тип и время
	oldTime := ""
	typ := ""
	if ov, exists := h.store.State.Overrides[dateStr]; exists && ov.Status != "cancelled" {
		oldTime = ov.NewTime
		typ = ov.Type
	} else {
		// Ищем в расписании
		d, _ := time.ParseInLocation("2006-01-02", dateStr, h.config.Timezone)
		for _, s := range h.store.Schedule {
			if s.DayOfWeek == d.Weekday() {
				oldTime = s.Time
				typ = s.Type
				break
			}
		}
	}
	if typ == "" {
		h.store.Unlock()
		h.vk.SendText(ctx, obj.Message.PeerID, "На эту дату нет занятия. Используйте `/plan`.", 0)
		return
	}

	// Помечаем старый опрос недействительным
	if old, exists := h.store.State.ActivePolls[dateStr]; exists {
		delete(h.store.State.ActivePolls, dateStr)
		// Пытаемся удалить сообщение (best-effort)
		go func() {
			_ = h.vk.DeleteMessage(context.Background(), old.MessageID)
		}()
	}
	delete(h.store.State.PendingPolls, dateStr) // сбрасываем возможный pending

	h.store.State.Overrides[dateStr] = &Override{
		Status:       "moved",
		Type:         typ,
		OriginalTime: oldTime,
		NewTime:      newTime,
		ChatId:       obj.Message.PeerID,
	}
	_ = h.store.SaveState()
	h.store.Unlock()

	h.vk.SendText(ctx, obj.Message.PeerID, fmt.Sprintf("⏰ Время изменено на %s. Старый опрос (если был) недействителен — создам новый при следующей проверке.", newTime), 0)
}

func (h *CommandHandler) handleCancel(ctx context.Context, obj events.MessageNewObject, args []string) {
	if len(args) < 1 {
		h.vk.SendText(ctx, obj.Message.PeerID, "Использование: `/cancel <дата> [причина]`", 0)
		return
	}
	dateStr, ok := parseDate(args[0], h.config.Timezone)
	if !ok {
		h.vk.SendText(ctx, obj.Message.PeerID, "Неверная дата.", 0)
		return
	}
	reason := ""
	if len(args) > 1 {
		reason = strings.Join(args[1:], " ")
	}

	h.store.Lock()
	// Определяем тип для записи override
	typ := ""
	if ov, exists := h.store.State.Overrides[dateStr]; exists && ov.Status != "cancelled" {
		typ = ov.Type
	} else {
		d, _ := time.ParseInLocation("2006-01-02", dateStr, h.config.Timezone)
		for _, s := range h.store.Schedule {
			if s.DayOfWeek == d.Weekday() {
				typ = s.Type
				break
			}
		}
	}
	if typ == "" {
		h.store.Unlock()
		h.vk.SendText(ctx, obj.Message.PeerID, "На эту дату нет занятия.", 0)
		return
	}

	// Удаляем активный опрос
	if old, exists := h.store.State.ActivePolls[dateStr]; exists {
		delete(h.store.State.ActivePolls, dateStr)
		go func() {
			_ = h.vk.DeleteMessage(context.Background(), old.MessageID)
		}()
	}
	delete(h.store.State.PendingPolls, dateStr)

	h.store.State.Overrides[dateStr] = &Override{
		Status: "cancelled",
		Type:   typ,
		Reason: reason,
		ChatId: obj.Message.PeerID,
	}
	_ = h.store.SaveState()
	h.store.Unlock()

	msg := fmt.Sprintf("❌ Занятие %s отменено.", dateStr)
	if reason != "" {
		msg += " Причина: " + reason
	}
	h.vk.SendText(ctx, obj.Message.PeerID, msg, 0)
}

// ---------- /who, /today, /week ----------

func (h *CommandHandler) handleWho(ctx context.Context, obj events.MessageNewObject, args []string) {
	dateStr := time.Now().In(h.config.Timezone).Format("2006-01-02")
	if len(args) > 0 {
		if d, ok := parseDate(args[0], h.config.Timezone); ok {
			dateStr = d
		}
	}

	h.store.Lock()
	ap, exists := h.store.State.ActivePolls[dateStr]
	h.store.Unlock()

	if !exists {
		h.vk.SendText(ctx, obj.Message.PeerID, "На эту дату активного опроса нет.", 0)
		return
	}

	voters, err := h.vk.GetVoters(ctx, ap.OwnerID, ap.PollID, ap.AnswerIDs)
	if err != nil {
		h.vk.SendText(ctx, obj.Message.PeerID, "⚠️ Не удалось получить голоса: "+err.Error(), 0)
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📊 Голоса на %s:\n", dateStr))
	labels := map[int]string{}
	for i, aid := range ap.AnswerIDs {
		labels[aid] = []string{"✅ Идут", "❌ Не идут", "🤔 Не уверены"}[i]
	}
	for _, aid := range ap.AnswerIDs {
		users := voters[aid]
		sb.WriteString(fmt.Sprintf("%s: %d\n", labels[aid], len(users)))
		if len(users) > 0 && len(users) <= 20 {
			sb.WriteString(fmt.Sprintf("  %v\n", users))
		}
	}
	h.vk.SendText(ctx, obj.Message.PeerID, sb.String(), 0)
}

func (h *CommandHandler) handleToday(ctx context.Context, obj events.MessageNewObject) {
	h.sendDayInfo(ctx, obj.Message.PeerID, time.Now().In(h.config.Timezone))
}

func (h *CommandHandler) handleWeek(ctx context.Context, obj events.MessageNewObject) {
	now := time.Now().In(h.config.Timezone)
	var sb strings.Builder
	sb.WriteString("🗓️ Расписание на неделю:\n")
	for i := 0; i < 7; i++ {
		d := now.AddDate(0, 0, i)
		dateStr := d.Format("2006-01-02")
		h.store.Lock()
		slot := resolveSlot(h.store.State, h.store.Schedule, dateStr, d)
		h.store.Unlock()
		if slot == nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("%s — %s %s\n", d.Format("02.01 (Mon)"), typeTitle(slot.Type), slot.Time))
	}
	if sb.Len() == len("🗓️ Расписание на неделю:\n") {
		sb.WriteString("На эту неделю занятий нет.")
	}
	h.vk.SendText(ctx, obj.Message.PeerID, sb.String(), 0)
}

func (h *CommandHandler) sendDayInfo(ctx context.Context, peerID int, d time.Time) {
	dateStr := d.Format("2006-01-02")
	h.store.Lock()
	slot := resolveSlot(h.store.State, h.store.Schedule, dateStr, d)
	h.store.Unlock()
	if slot == nil {
		h.vk.SendText(ctx, peerID, fmt.Sprintf("На %s занятий нет.", dateStr), 0)
		return
	}
	h.vk.SendText(ctx, peerID, fmt.Sprintf("📌 %s — %s %s.", dateStr, typeTitle(slot.Type), slot.Time), 0)
}

// ---------- Утилиты ----------

func parseDay(s string) (time.Weekday, bool) {
	m := map[string]time.Weekday{
		"пн": time.Monday, "monday": time.Monday, "mon": time.Monday,
		"вт": time.Tuesday, "tuesday": time.Tuesday, "tue": time.Tuesday,
		"ср": time.Wednesday, "wednesday": time.Wednesday, "wed": time.Wednesday,
		"чт": time.Thursday, "thursday": time.Thursday, "thu": time.Thursday,
		"пт": time.Friday, "friday": time.Friday, "fri": time.Friday,
		"сб": time.Saturday, "saturday": time.Saturday, "sat": time.Saturday,
		"вс": time.Sunday, "sunday": time.Sunday, "sun": time.Sunday,
	}
	d, ok := m[strings.ToLower(s)]
	return d, ok
}

func parseType(s string) (string, bool) {
	m := map[string]string{
		"бассейн": "pool", "бас": "pool", "pool": "pool",
		"зал": "gym", "спортзал": "gym", "gym": "gym",
		"остров": "island", "island": "island",
		"лекция": "lecture", "лек": "lecture", "lecture": "lecture",
	}
	t, ok := m[strings.ToLower(s)]
	return t, ok
}

func parseTime(s string) (string, bool) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return "", false
	}
	return t.Format("15:04"), true
}

func parseDate(s string, loc *time.Location) (string, bool) {
	now := time.Now().In(loc)
	switch strings.ToLower(s) {
	case "сегодня", "today":
		return now.Format("2006-01-02"), true
	case "завтра", "tomorrow":
		return now.AddDate(0, 0, 1).Format("2006-01-02"), true
	}
	// ДД.ММ
	if t, err := time.ParseInLocation("02.01", s, loc); err == nil {
		// Если дата уже прошла в этом году — берём следующий год
		t = t.AddDate(now.Year(), 0, 0)
		if t.Before(now.Truncate(24 * time.Hour)) {
			t = t.AddDate(1, 0, 0)
		}
		return t.Format("2006-01-02"), true
	}
	// ГГГГ-ММ-ДД
	if t, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
		return t.Format("2006-01-02"), true
	}
	return "", false
}

func typeTitle(t string) string {
	m := map[string]string{"pool": "Бассейн", "gym": "Спортзал", "island": "Остров", "lecture": "Лекция"}
	return m[t]
}

func typeEmoji(t string) string {
	m := map[string]string{"pool": "🏊", "gym": "🏋️", "island": "🏝️", "lecture": "📚"}
	return m[t]
}

func dayName(d time.Weekday) string {
	m := map[time.Weekday]string{
		time.Monday: "Пн", time.Tuesday: "Вт", time.Wednesday: "Ср",
		time.Thursday: "Чт", time.Friday: "Пт", time.Saturday: "Сб", time.Sunday: "Вс",
	}
	return m[d]
}

func formatDateHuman(dateStr string) string {
	t, _ := time.Parse("2006-01-02", dateStr)
	return t.Format("02.01")
}

func endDateFor(dateStr string, loc *time.Location) int {
	t, _ := time.ParseInLocation("2006-01-02", dateStr, loc)
	// Конец дня занятия
	return int(t.Add(23*time.Hour + 59*time.Minute).Unix())
}

func randomIDFor(dateStr string, attempt int) int {
	h := 0
	for _, c := range dateStr {
		h = 31*h + int(c)
	}
	return h*100 + attempt
}
