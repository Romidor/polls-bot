package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/SevereCloud/vksdk/v3/api"
	"github.com/SevereCloud/vksdk/v3/api/params"
)

type VKClient struct {
	VK *api.VK
}

func NewVKClient(token string) *VKClient {
	return &VKClient{VK: api.NewVK(token)}
}

// CreatePoll создаёт опрос от имени сообщества.
func (c *VKClient) CreatePoll(ctx context.Context, ownerID int, question string, answers []string, endDate int) (pollID int, answerIDs []int, err error) {
	for i, answer := range answers {
		answers[i] = strconv.Quote(answer)
	}
	jsonAnswers := "[" + strings.Join(answers, ",") + "]"

	b := params.NewPollsCreateBuilder()
	b.OwnerID(ownerID)
	b.Question(question)
	b.AddAnswers(jsonAnswers)
	b.IsAnonymous(false)
	b.IsMultiple(false)
	b.EndDate(endDate)

	res, err := c.VK.PollsCreate(b.Params)
	if err != nil {
		return 0, nil, err
	}

	// vksdk обычно возвращает object.PollsCreateResponse с полем Poll.
	// Если структура отличается, проверьте res.Poll.ID / res.ID.
	pollID = res.ID
	for _, a := range res.Answers {
		answerIDs = append(answerIDs, a.ID)
	}
	return pollID, answerIDs, nil
}

// SendPoll отправляет сообщение с опросом в чат. Возвращает ID сообщения.
func (c *VKClient) SendPoll(ctx context.Context, peerID, ownerID, pollID int, message string, randomID int) (int, error) {
	b := params.NewMessagesSendBuilder()
	b.PeerID(peerID)
	b.Message(message)
	b.Attachment(fmt.Sprintf("poll%d_%d", ownerID, pollID))
	b.RandomID(randomID)

	res, err := c.VK.MessagesSend(b.Params)
	if err != nil {
		return 0, err
	}
	return res, nil
}

// DeleteMessage удаляет сообщение (best-effort).
func (c *VKClient) DeleteMessage(ctx context.Context, messageID ...int) error {
	b := params.NewMessagesDeleteBuilder()
	b.MessageIDs(messageID)
	b.DeleteForAll(true)
	_, err := c.VK.MessagesDelete(b.Params)
	return err
}

// GetVoters возвращает map[answerID][]userID.
func (c *VKClient) GetVoters(ctx context.Context, ownerID, pollID int, answerIDs []int) (map[int][]int, error) {
	b := params.NewPollsGetVotersBuilder()
	b.OwnerID(ownerID)
	b.PollID(pollID)
	b.AnswerIDs(answerIDs)

	res, err := c.VK.PollsGetVoters(b.Params)
	if err != nil {
		return nil, err
	}

	result := make(map[int][]int)
	for _, item := range res {
		result[item.AnswerID] = item.Users.Items
	}
	return result, nil
}

// sendText — вспомогательная отправка текста.
func (c *VKClient) SendText(ctx context.Context, peerID int, text string, randomID int) (int, error) {
	b := params.NewMessagesSendBuilder()
	b.PeerID(peerID)
	b.Message(text)
	b.RandomID(randomID)
	return c.VK.MessagesSend(b.Params)
}

// --- Retry и классификация ошибок ---

func retry(ctx context.Context, maxAttempts int, f func() error) error {
	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		err := f()
		if err == nil {
			return nil
		}
		lastErr = err

		if !isRetryable(err) {
			break
		}

		delay := time.Duration(1<<uint(i)) * time.Second
		log.Printf("retryable error (attempt %d/%d): %v, sleeping %v", i+1, maxAttempts, err, delay)

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return lastErr
}

func isRetryable(err error) bool {
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case 6, 10, 1: // Too many requests, Internal server error, Unknown
			return true
		}
	}
	// Также retryable при сетевых ошибках (упрощённо)
	return false
}

func isFatalPermission(err error) bool {
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case 7, 15: // Permission denied, Access denied
			return true
		}
	}
	return false
}
