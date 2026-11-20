package main

import "time"

type ScheduleSlot struct {
	DayOfWeek time.Weekday `json:"day_of_week"`
	Type      string       `json:"type"`  // pool, gym, island, lecture
	Time      string       `json:"time"`  // "20:00"
	Title     string       `json:"title"` // человекочитаемое название
}

type Override struct {
	Status       string `json:"status"` // moved, cancelled, extra
	Type         string `json:"type"`
	OriginalTime string `json:"original_time,omitempty"`
	NewTime      string `json:"new_time,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

type ActivePoll struct {
	PollID    int   `json:"poll_id"`
	OwnerID   int   `json:"owner_id"`
	MessageID int   `json:"message_id"`
	ChatID    int   `json:"chat_id"`
	AnswerIDs []int `json:"answer_ids,omitempty"`
}

type PendingPoll struct {
	Date      string `json:"date"`
	Type      string `json:"type"`
	Time      string `json:"time"`
	ChatID    int    `json:"chat_id"`
	PollID    int    `json:"poll_id,omitempty"` // уже созданный опрос, если CreatePoll прошёл
	AnswerIDs []int  `json:"answer_ids,omitempty"`
	Attempts  int    `json:"attempts"`
	CreatedAt string `json:"created_at"`
}

type State struct {
	Version      int                     `json:"version"`
	LongPollTs   string                  `json:"long_poll_ts,omitempty"`
	Overrides    map[string]*Override    `json:"overrides"`
	ActivePolls  map[string]*ActivePoll  `json:"active_polls"`
	PendingPolls map[string]*PendingPoll `json:"pending_polls"`
}

func NewState() *State {
	return &State{
		Version:      1,
		Overrides:    make(map[string]*Override),
		ActivePolls:  make(map[string]*ActivePoll),
		PendingPolls: make(map[string]*PendingPoll),
	}
}
