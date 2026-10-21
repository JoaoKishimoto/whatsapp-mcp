package wa

import (
	"crypto/sha256"
	"time"

	"go.mau.fi/whatsmeow/types/events"
)

// PollVote is the poll_vote event (PLANO.md §8).
type PollVote struct {
	PollMessageID string    `json:"poll_message_id"`
	ChatJID       string    `json:"chat_jid"`
	Voter         Person    `json:"voter"`
	Selected      []string  `json:"selected"` // empty when the voter unchecked everything
	Timestamp     time.Time `json:"timestamp"`
}

// Vote builds the event from a decrypted vote. A vote carries SHA-256 hashes
// of the chosen option names; options are the names the gateway sent.
func Vote(evt *events.Message, options []string, hashes [][]byte) PollVote {
	return PollVote{
		PollMessageID: evt.Message.GetPollUpdateMessage().GetPollCreationMessageKey().GetID(),
		ChatJID:       evt.Info.Chat.String(),
		Voter:         SenderOf(evt.Info.MessageSource),
		Selected:      SelectedOptions(options, hashes),
		Timestamp:     evt.Info.Timestamp,
	}
}

// SelectedOptions returns, in poll order, the options whose hash is in hashes.
func SelectedOptions(options []string, hashes [][]byte) []string {
	chosen := make(map[[sha256.Size]byte]bool, len(hashes))
	for _, h := range hashes {
		if len(h) == sha256.Size {
			chosen[[sha256.Size]byte(h)] = true
		}
	}
	selected := []string{}
	for _, o := range options {
		if chosen[sha256.Sum256([]byte(o))] {
			selected = append(selected, o)
		}
	}
	return selected
}
