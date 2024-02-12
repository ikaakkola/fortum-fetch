package main

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/chromedp/chromedp"
)

// Path to login
const loginPath = "/login?lang=en"

// Form elements in My Fortum
const userNameInput = `//input[@id="ttqusername"]`
const passwordInput = `//input[@id="user-password"]`
const submitButton = `a.btn--login`

type Auth struct {
	User     string
	Password string
	LoginUrl string
	Token    string
}

func NewAuth(user *string, password *string, baseUrl *string) (*Auth, error) {
	a := Auth{}
	if user == nil || *user == "" {
		return nil, errors.New("user cannot be empty")
	}
	if password == nil || *password == "" {
		return nil, errors.New("password cannot be empty")
	}
	if baseUrl == nil || *baseUrl == "" {
		return nil, errors.New("url cannot be empty")
	}
	a.User = *user
	a.Password = *password
	a.LoginUrl = *baseUrl + loginPath
	return &a, nil
}

func getAccessToken(auth *Auth) (*string, error) {
	if cli.Debug {
		log.Printf("create new browser context, headless %v", cli.Headless)
	}
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = chromedp.NewExecAllocator(
		context.Background(),
		append(
			chromedp.DefaultExecAllocatorOptions[:],
			chromedp.Flag("headless", cli.Headless),
			chromedp.Flag("disable-gpu", true),
		)...,
	)
	defer cancel()

	ctx, cancel = chromedp.NewContext(ctx, chromedp.WithLogf(log.Printf))
	defer cancel()

	// Open the login page, fill the login form
	if cli.Debug {
		log.Printf("navigate to %s", auth.LoginUrl)
	}
	err := chromedp.Run(ctx,
		runWithTimeOut(&ctx, 10, chromedp.Tasks{
			chromedp.Navigate(auth.LoginUrl),
			chromedp.WaitReady(`body`),
		}),
	)
	if err != nil {
		cancel()
		return nil, err
	}
	if cli.Debug {
		log.Printf("login as %s", auth.User)
	}
	err = chromedp.Run(ctx,
		runWithTimeOut(&ctx, 10, chromedp.Tasks{
			chromedp.Sleep(time.Second * 1), // the Wicket javascript seems to connect to elements slowly
			chromedp.SendKeys(userNameInput, auth.User),
			chromedp.SendKeys(passwordInput, auth.Password),
		}),
	)
	if err != nil {
		cancel()
		return nil, err
	}

	// Submit login form
	err = chromedp.Run(ctx, runWithTimeOut(&ctx, 10, chromedp.Tasks{
		chromedp.WaitReady(submitButton),
		chromedp.Click(submitButton, chromedp.ByQuery),
		chromedp.WaitNotPresent(userNameInput),
	}))
	if err != nil {
		cancel()
		return nil, errors.Join(errors.New("login failed"), err)
	}

	// Read access token from session storage
	accessToken, err := readAccessToken(ctx)
	if err != nil {
		cancel()
		return nil, err
	}

	return accessToken, nil
}

func readAccessToken(ctx context.Context) (*string, error) {
	attempt := 1

	// Try until accessToken available in session storage, failing after 10 seconds
	var err error = nil
	var accessToken *string = nil
	for attempt < 100 {
		err = chromedp.Run(ctx, chromedp.Tasks{
			chromedp.WaitReady(`body`),
			chromedp.Evaluate("sessionStorage.getItem('accessToken')", &accessToken),
		})
		if accessToken != nil {
			return accessToken, nil
		}
		attempt += 1
		time.Sleep(time.Millisecond * 100)
	}
	if err != nil {
		return nil, errors.Join(errors.New("failed to get accessToken"), err)
	}
	return nil, errors.New("accessToken not found")
}

func runWithTimeOut(ctx *context.Context, timeout time.Duration, tasks chromedp.Tasks) chromedp.ActionFunc {
	return func(ctx context.Context) error {
		timeoutContext, cancel := context.WithTimeout(ctx, timeout*time.Second)
		defer cancel()
		return tasks.Do(timeoutContext)
	}
}
