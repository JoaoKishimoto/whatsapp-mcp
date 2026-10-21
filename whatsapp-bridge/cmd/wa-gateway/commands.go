package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mdp/qrterminal"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"whatsapp-client/internal/api"
)

const usage = `usage: wa-gateway [serve | pair [--phone <bot number>] | health [url]]

  serve    run the gateway (default)
  pair     link this gateway to the bot's WhatsApp: with --phone, by an 8-character
           code typed on the phone; without it, by a QR code
  health   exit 0 if the running gateway is connected and logged in (Docker healthcheck)

The data directory is STORE_DIR (default ./store). whatsapp.db in it holds the session keys.`

// storeDir holds whatsapp.db (the session keys), messages.db, events.db, polls.db and media.db.
var storeDir = "store"

// mediaDir is the volume shared with the bridge: downloads land here, files are sent only from here.
var mediaDir = "media"

func storePath(name string) string { return filepath.Join(storeDir, name) }

// useColor is off under NO_COLOR (set in the Docker image), so logs carry no escape codes.
var useColor = os.Getenv("NO_COLOR") == ""

func main() {
	logger := waLog.Stdout("Client", "INFO", useColor)
	if dir := os.Getenv("STORE_DIR"); dir != "" {
		storeDir = dir
	}
	if dir := os.Getenv("MEDIA_DIR"); dir != "" {
		mediaDir = dir
	}
	// Absolute, so a path returned by /v1/media/download can be sent back as is.
	if dir, err := filepath.Abs(mediaDir); err == nil {
		mediaDir = dir
	}
	cmd, args := "serve", os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve(logger)
	case "pair":
		err = pair(logger, args)
	case "health":
		err = health(args)
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		logger.Errorf("%v", err)
		os.Exit(1)
	}
}

// openClient opens the session store and returns a client that is not connected yet.
func openClient(logger waLog.Logger) (*whatsmeow.Client, error) {
	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		return nil, fmt.Errorf("create store directory: %w", err)
	}
	dsn := "file:" + storePath("whatsapp.db") + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	container, err := sqlstore.New(context.Background(), "sqlite", dsn, waLog.Stdout("Database", "INFO", useColor))
	if err != nil {
		return nil, fmt.Errorf("open session store: %w", err)
	}
	device, err := container.GetFirstDevice(context.Background())
	if err != nil {
		return nil, fmt.Errorf("read session: %w", err)
	}
	return whatsmeow.NewClient(device, logger), nil
}

// pair links a new session to the bot's account (PLANO.md §9, G2). Run it once, with serve stopped.
func pair(logger waLog.Logger, args []string) error {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	phone := fs.String("phone", "", "the bot's number, digits only with country code (e.g. 5571999999999); without it, a QR code is shown")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *phone != "" && !validPhone(*phone) {
		return errors.New("--phone must be digits only, with country and area code (e.g. 5571999999999)")
	}
	client, err := openClient(logger)
	if err != nil {
		return err
	}
	if client.Store.ID != nil {
		return errors.New("already paired. To pair again, remove this device on the bot's phone " +
			"(Linked devices), then delete whatsapp.db from " + storeDir)
	}
	store.DeviceProps.Os = proto.String("wa-gateway") // the name the phone lists under Linked devices

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	qrChan, err := client.GetQRChannel(ctx)
	if err != nil {
		return err
	}
	connected := make(chan struct{}, 1)
	client.AddEventHandler(func(evt any) {
		if _, ok := evt.(*events.Connected); ok {
			select {
			case connected <- struct{}{}:
			default:
			}
		}
	})
	if err := client.Connect(); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer client.Disconnect()

	codeShown := false
	for item := range qrChan {
		switch item.Event {
		case whatsmeow.QRChannelEventCode:
			if *phone == "" {
				fmt.Println("\nOn the bot's phone: WhatsApp > Linked devices > Link a device, and scan:")
				qrterminal.GenerateHalfBlock(item.Code, qrterminal.L, os.Stdout)
				continue
			}
			if codeShown {
				continue // new QR codes keep coming; the pairing code stays valid until they run out
			}
			// The first QR code means the connection is up, which PairPhone needs.
			code, err := client.PairPhone(ctx, *phone, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
			if err != nil {
				return fmt.Errorf("request pairing code: %w", err)
			}
			codeShown = true
			fmt.Printf("\nPairing code: %s\n\n", code)
			fmt.Println("On the bot's phone: WhatsApp > Linked devices > Link a device >")
			fmt.Println("Link with phone number instead, and type the code. You have about 2 minutes.")
		case whatsmeow.QRChannelSuccess.Event:
			fmt.Println("\nPaired. Finishing the first connection...")
			select {
			case <-connected:
			case <-time.After(30 * time.Second):
				return errors.New("paired, but the first connection did not complete; start `serve` and check its log")
			}
			time.Sleep(5 * time.Second) // let whatsmeow upload its keys before disconnecting
			fmt.Println("Done. Start the gateway with `wa-gateway serve`.")
			return nil
		case whatsmeow.QRChannelTimeout.Event:
			return errors.New("pairing timed out; run `pair` again")
		case whatsmeow.QRChannelEventPasskeyRequest:
			return errors.New("WhatsApp asked for a passkey to link this device, which the gateway does not support")
		default:
			return fmt.Errorf("pairing failed: %s %v", item.Event, item.Error)
		}
	}
	return errors.New("pairing ended without success")
}

// retentionFromEnv reads MESSAGE_RETENTION_DAYS (default 7): how long message content,
// attachment keys and downloaded files are kept (G11).
func retentionFromEnv() (time.Duration, error) {
	days := 7
	if v := os.Getenv("MESSAGE_RETENTION_DAYS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return 0, errors.New("MESSAGE_RETENTION_DAYS must be a whole number of days, at least 1")
		}
		days = n
	}
	return time.Duration(days) * 24 * time.Hour, nil
}

// validPhone accepts an international number as digits only: no "+", spaces or leading 0.
func validPhone(s string) bool {
	if len(s) < 8 || len(s) > 15 || s[0] == '0' {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// health checks the running gateway through its own /v1/health. distroless has no curl.
func health(args []string) error {
	url := "http://127.0.0.1:8080/v1/health"
	if len(args) > 0 {
		url = args[0]
	}
	c := http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var h api.Health
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&h) != nil {
		return fmt.Errorf("health check: HTTP %d", resp.StatusCode)
	}
	if !h.Connected || !h.LoggedIn {
		return fmt.Errorf("WhatsApp not ready: connected=%v logged_in=%v", h.Connected, h.LoggedIn)
	}
	return nil
}
