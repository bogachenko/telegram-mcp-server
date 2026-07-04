// Package telegram owns Telegram MTProto client wiring.
package telegram

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gotd/td/session"
	gotdtelegram "github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
)

// Config contains Telegram MTProto client settings.
type Config struct {
	APIID       int
	APIHash     string
	Phone       string
	Password    string
	SessionPath string
}

// Client wraps Telegram MTProto auth and identity checks.
type Client struct {
	config Config
}

// Self describes the authorized Telegram user.
type Self struct {
	ID        int64
	Username  string
	FirstName string
	LastName  string
	Phone     string
	Bot       bool
}

// NewClient creates a Telegram client wrapper.
func NewClient(cfg Config) *Client {
	return &Client{config: cfg}
}

// Auth runs interactive user-client authentication and returns the authorized user.
func (c *Client) Auth(ctx context.Context, stdin io.Reader, stdout io.Writer) (Self, error) {
	if c == nil {
		return Self{}, fmt.Errorf("telegram client is required")
	}
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if err := c.config.validateAuth(); err != nil {
		return Self{}, err
	}

	client, err := c.newGotdClient()
	if err != nil {
		return Self{}, err
	}

	codeAsk := func(ctx context.Context, sentCode *tg.AuthSentCode) (string, error) {
		_, _ = fmt.Fprint(stdout, "telegram code: ")
		return readLine(stdin)
	}

	authenticator := auth.UserAuthenticator(auth.CodeOnly(
		c.config.Phone,
		auth.CodeAuthenticatorFunc(codeAsk),
	))
	if c.config.Password != "" {
		authenticator = auth.Constant(
			c.config.Phone,
			c.config.Password,
			auth.CodeAuthenticatorFunc(codeAsk),
		)
	}

	var self Self
	err = client.Run(ctx, func(ctx context.Context) error {
		flow := auth.NewFlow(authenticator, auth.SendCodeOptions{})
		if err := client.Auth().IfNecessary(ctx, flow); err != nil {
			return fmt.Errorf("telegram auth flow: %w", err)
		}

		user, err := client.Self(ctx)
		if err != nil {
			return fmt.Errorf("telegram self: %w", err)
		}

		self = mapSelf(user)
		return nil
	})
	if err != nil {
		if c.config.Password == "" && strings.Contains(err.Error(), "password requested") {
			return Self{}, fmt.Errorf("%w; set TGMCP_TELEGRAM_PASSWORD if two-step verification is enabled", err)
		}
		return Self{}, err
	}

	return self, nil
}

// Me returns the current authorized Telegram user from saved session.
func (c *Client) Me(ctx context.Context) (Self, bool, error) {
	if c == nil {
		return Self{}, false, fmt.Errorf("telegram client is required")
	}
	if err := c.config.validateBase(); err != nil {
		return Self{}, false, err
	}

	client, err := c.newGotdClient()
	if err != nil {
		return Self{}, false, err
	}

	var self Self
	var authorized bool
	err = client.Run(ctx, func(ctx context.Context) error {
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return fmt.Errorf("telegram auth status: %w", err)
		}
		if !status.Authorized {
			return nil
		}

		authorized = true
		if status.User != nil {
			self = mapSelf(status.User)
			return nil
		}

		user, err := client.Self(ctx)
		if err != nil {
			return fmt.Errorf("telegram self: %w", err)
		}
		self = mapSelf(user)
		return nil
	})
	if err != nil {
		return Self{}, false, err
	}

	return self, authorized, nil
}

// DisplayName returns a readable Telegram name.
func (s Self) DisplayName() string {
	return strings.TrimSpace(strings.Join([]string{s.FirstName, s.LastName}, " "))
}

func (c *Client) newGotdClient() (*gotdtelegram.Client, error) {
	if err := c.config.ensureSessionDir(); err != nil {
		return nil, err
	}

	return gotdtelegram.NewClient(c.config.APIID, c.config.APIHash, gotdtelegram.Options{
		SessionStorage: &session.FileStorage{
			Path: c.config.SessionPath,
		},
	}), nil
}

func (c Config) validateAuth() error {
	if err := c.validateBase(); err != nil {
		return err
	}
	if strings.TrimSpace(c.Phone) == "" {
		return fmt.Errorf("TGMCP_TELEGRAM_PHONE is required for telegram-auth")
	}
	return nil
}

func (c Config) validateBase() error {
	if c.APIID == 0 {
		return fmt.Errorf("TGMCP_TELEGRAM_API_ID is required")
	}
	if strings.TrimSpace(c.APIHash) == "" {
		return fmt.Errorf("TGMCP_TELEGRAM_API_HASH is required")
	}
	if strings.TrimSpace(c.SessionPath) == "" {
		return fmt.Errorf("telegram session path is required")
	}
	return nil
}

func (c Config) ensureSessionDir() error {
	dir := filepath.Dir(c.SessionPath)
	if dir == "" || dir == "." {
		return nil
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create telegram session dir: %w", err)
	}
	return nil
}

func readLine(input io.Reader) (string, error) {
	line, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func mapSelf(user *tg.User) Self {
	if user == nil {
		return Self{}
	}
	return Self{
		ID:        user.ID,
		Username:  user.Username,
		FirstName: user.FirstName,
		LastName:  user.LastName,
		Phone:     user.Phone,
		Bot:       user.Bot,
	}
}
