package main

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// Login button in landing page
const loginButton = `//button[@data-testid="loginButton"]`

// Form elements in Login page. The SSO flow asks for the user name and the
// password on two separate pages, both fields are named callback_1. The form
// is submitted by pressing enter in the field, the submit button texts differ
// between pages and languages.
const userNameInput = `//input[@name="callback_1"][@type="text"]`
const passwordInput = `//input[@name="callback_1"][@type="password"]`

type Auth struct {
	User     string
	Password string
	BaseUrl  string
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
	a.BaseUrl = *baseUrl
	if !strings.Contains(a.BaseUrl, "/fi/sahkoa") && !strings.Contains(a.BaseUrl, "/se/el") {
		return nil, errors.New("Invalid base url")
	}
	return &a, nil
}

func getAccessCookies(auth *Auth) ([]*network.Cookie, error) {
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
			chromedp.WindowSize(1600, 1200),
			chromedp.Flag("disable-gpu", true),
		)...,
	)
	defer cancel()

	ctx, cancel = chromedp.NewContext(ctx, chromedp.WithLogf(log.Printf)) //  chromedp.WithDebugf(log.Printf))
	defer cancel()

	// Open the login page, fill the login form
	if cli.Debug {
		log.Printf("navigate to %s", auth.BaseUrl)
	}

	err := chromedp.Run(ctx,
		runWithTimeOut(&ctx, 30, chromedp.Tasks{
			chromedp.Navigate(auth.BaseUrl),
			chromedp.WaitReady(loginButton),
			// a javascript click so that the cookie banner cannot cover the button
			chromedp.Evaluate(`document.querySelector('button[data-testid="loginButton"]').click()`, nil),
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
		runWithTimeOut(&ctx, 30, chromedp.Tasks{
			chromedp.Sleep(time.Second * 3), // the Wicket javascript seems to connect to elements slowly
			chromedp.WaitReady(userNameInput),
			chromedp.SendKeys(userNameInput, auth.User+"\r"),
			chromedp.WaitReady(passwordInput),
			chromedp.SendKeys(passwordInput, auth.Password+"\r"),
		}),
	)
	if err != nil {
		cancel()
		return nil, errors.Join(errors.New("login failed"), err)
	}

	if cli.Debug {
		log.Printf("logged in as %s, reading cookies", auth.User)
	}

	// get the auth cookie
	var cookies []*network.Cookie
	cookies, err = getCookies(ctx)
	if err != nil {
		cancel()
		return nil, err
	}

	return cookies, nil
}

func getCookies(ctx context.Context) ([]*network.Cookie, error) {
	for attempt := 1; attempt < 100; attempt++ {
		var result []*network.Cookie
		err := chromedp.Run(ctx,
			chromedp.ActionFunc(func(ctx context.Context) error {
				cookies, err := network.GetCookies().Do(ctx)
				if err != nil {
					return err
				}

				for _, v := range cookies {
					if strings.HasPrefix(v.Name, "__Secure-next-auth.session-token") {
						if cli.Debug {
							log.Printf("found auth cookie %v", v.Name)
						}
						result = append(result, v)
					}
				}
				return nil
			}),
		)

		if err != nil {
			return nil, err
		}

		// a large session cookie is split into multiple chunks
		if len(result) > 0 {
			return result, nil
		}

		time.Sleep(time.Millisecond * 100)
	}

	return nil, errors.New("Did not find authentication cookies")
}

func runWithTimeOut(ctx *context.Context, timeout time.Duration, tasks chromedp.Tasks) chromedp.ActionFunc {
	return func(ctx context.Context) error {
		timeoutContext, cancel := context.WithTimeout(ctx, timeout*time.Second)
		defer cancel()
		return tasks.Do(timeoutContext)
	}
}
