package main

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow/types"
)

// Chat methods of waClient (api.Chat).

func (w waClient) SetTyping(ctx context.Context, chat types.JID, typing bool) error {
	state := types.ChatPresencePaused
	if typing {
		state = types.ChatPresenceComposing
	}
	return w.c.SendChatPresence(ctx, chat, state, types.ChatPresenceMediaText)
}

func (w waClient) React(ctx context.Context, chat, sender types.JID, messageID, emoji string) (string, error) {
	resp, err := w.c.SendMessage(ctx, chat, w.c.BuildReaction(chat, sender, messageID, emoji))
	return resp.ID, err
}

func (w waClient) MarkRead(ctx context.Context, chat, sender types.JID, messageIDs []string) error {
	return w.c.MarkRead(ctx, messageIDs, time.Now(), chat, sender)
}
