package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// Login button in landing page
const loginButton = `//button[@data-testid="loginButton"]`

// Form elements in Login page
const userNameInput = `//input[@name="callback_1"]`
const passwordInput = `//input[@name="callback_2"]`
const submitButton = `//button[text()="%v"]`

type Auth struct {
	User      string
	Password  string
	BaseUrl   string
	Token     string
	LoginText string
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
	if strings.Contains(a.BaseUrl, "/fi/sahkoa/sv") {
		a.LoginText = "Logga in"
	} else if strings.Contains(a.BaseUrl, "/fi/sahkoa/en") {
		a.LoginText = "Log in"
	} else if strings.Contains(a.BaseUrl, "/se/el") {
		a.LoginText = "Logga in"
	} else if strings.Contains(a.BaseUrl, "/fi/sahkoa") {
		a.LoginText = "Kirjaudu"
	} else {
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

	var res []string
	err := chromedp.Run(ctx,
		runWithTimeOut(&ctx, 30, chromedp.Tasks{
			chromedp.Navigate(auth.BaseUrl),
			chromedp.WaitReady(`body`),
			chromedp.Click(`//button[@id="onetrust-reject-all-handler"]`),
			chromedp.Evaluate(`document.querySelector('button[data-testid="loginButton"]').click()`, &res),
		}),
	)
	if err != nil {
		cancel()
		return nil, err
	}

	err = chromedp.Run(ctx)
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
		chromedp.WaitReady(fmt.Sprintf(submitButton, auth.LoginText)),
		chromedp.Click(fmt.Sprintf(submitButton, auth.LoginText)),
		chromedp.WaitNotPresent(passwordInput),
	}))

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
	attempt := 1

	var result []*network.Cookie

	var err error = nil
	for attempt < 100 {
		err = chromedp.Run(ctx,
			chromedp.WaitReady(`body`),
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

		if len(result) == 2 {
			return result, nil
		}

		attempt += 1
		time.Sleep(time.Millisecond * 100)
	}

	if err != nil {
		return nil, err
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
