package wa

import "go.mau.fi/whatsmeow/types/events"

// Connection is a change in the link to WhatsApp, queued as a "connection" event (PLANO.md §8, G10).
type Connection struct {
	State  string `json:"state"`            // connected | disconnected | logged_out | temporary_ban | client_outdated | stream_replaced | connect_failure
	Reason string `json:"reason,omitempty"` // WhatsApp's reason, when it gives one
}

// ConnectionOf maps whatsmeow's connection events; ok is false for any other event.
func ConnectionOf(evt any) (Connection, bool) {
	switch v := evt.(type) {
	case *events.Connected:
		return Connection{State: "connected"}, true
	case *events.Disconnected:
		return Connection{State: "disconnected"}, true
	case *events.LoggedOut:
		return Connection{State: "logged_out", Reason: v.Reason.String()}, true
	case *events.TemporaryBan:
		return Connection{State: "temporary_ban", Reason: v.String()}, true
	case *events.ClientOutdated:
		return Connection{State: "client_outdated"}, true
	case *events.StreamReplaced:
		return Connection{State: "stream_replaced"}, true
	case *events.ConnectFailure:
		// whatsmeow sends this only for failures it does not retry by itself.
		reason := v.Reason.String()
		if v.Message != "" {
			reason += ": " + v.Message
		}
		return Connection{State: "connect_failure", Reason: reason}, true
	}
	return Connection{}, false
}

// Fatal reports states whatsmeow does not recover from on its own: someone has to act.
// A plain disconnection is retried automatically.
func (c Connection) Fatal() bool {
	return c.State != "connected" && c.State != "disconnected"
}
