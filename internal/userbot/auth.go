package userbot

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
)

// Self is a small display wrapper around the connected account.
type Self struct {
	User *tg.User
}

// Display returns a human-friendly name for the account.
func (s *Self) Display() string {
	if s == nil || s.User == nil {
		return "?"
	}
	name := strings.TrimSpace(s.User.FirstName + " " + s.User.LastName)
	if s.User.Username != "" {
		if name != "" {
			name += " "
		}
		name += "@" + s.User.Username
	}
	if name == "" {
		name = "ID " + itoa64(s.User.ID)
	}
	return name
}

// LoginSession bridges the asynchronous Telegram-bot conversation (where the
// user types the code/password across several messages) with gotd's
// synchronous auth flow callbacks. The callbacks are invoked from the gotd
// goroutine; the bot fills the channels from its handler goroutine.
type LoginSession struct {
	Phone string

	codeCh chan string
	pwCh   chan string

	// Notify* are set by the bot layer before the flow starts. They are called
	// exactly when gotd needs the corresponding input.
	NotifyCode     func()
	NotifyPassword func()
	NotifyDone     func(self *Self)
	NotifyError    func(err error)
}

func itoa64(i int64) string { return strconv.FormatInt(i, 10) }

func newLoginSession(phone string) *LoginSession {
	return &LoginSession{
		Phone:  phone,
		codeCh: make(chan string, 1),
		pwCh:   make(chan string, 1),
	}
}

// SubmitCode delivers the login code typed by the user.
func (s *LoginSession) SubmitCode(code string) {
	select {
	case s.codeCh <- code:
	default:
	}
}

// SubmitPassword delivers the 2FA password typed by the user.
func (s *LoginSession) SubmitPassword(pw string) {
	select {
	case s.pwCh <- pw:
	default:
	}
}

// interactiveAuth implements auth.UserAuthenticator backed by a LoginSession.
type interactiveAuth struct {
	s *LoginSession
}

func (a interactiveAuth) Phone(_ context.Context) (string, error) {
	return a.s.Phone, nil
}

func (a interactiveAuth) Code(ctx context.Context, _ *tg.AuthSentCode) (string, error) {
	if a.s.NotifyCode != nil {
		a.s.NotifyCode()
	}
	select {
	case c := <-a.s.codeCh:
		return c, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (a interactiveAuth) Password(ctx context.Context) (string, error) {
	if a.s.NotifyPassword != nil {
		a.s.NotifyPassword()
	}
	select {
	case p := <-a.s.pwCh:
		return p, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (a interactiveAuth) AcceptTermsOfService(_ context.Context, _ tg.HelpTermsOfService) error {
	return nil
}

func (a interactiveAuth) SignUp(_ context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("this account is not registered; sign-up is not supported")
}
