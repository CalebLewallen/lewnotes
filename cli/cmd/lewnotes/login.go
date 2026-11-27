package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/caleblewallen/lewnotes/cli/client"
	"github.com/caleblewallen/lewnotes/cli/internal/config"
)

// loginError carries the machine-readable error code for a failed handoff.
type loginError struct {
	code    string
	message string
}

func (e *loginError) Error() string { return e.message }

func runLogin(a *app, args []string) int {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	tokenStdin := fs.Bool("token-stdin", false, "read token from stdin")
	open := fs.Bool("open", false, "open the approval page in a browser")
	noBrowser := fs.Bool("no-browser", false, "use the device code flow instead of a local browser")
	noOpen := fs.Bool("no-open", false, "never open a browser (alias for --no-browser)")
	deviceFlag := fs.String("device-name", "", "name to show on the approval page")
	rest, err := parseCommandFlags(fs, args)
	if err != nil {
		return a.out.fail(err)
	}
	if len(rest) > 1 {
		return a.out.fail(&usageError{message: "login takes at most one server URL"})
	}
	positional := ""
	if len(rest) == 1 {
		positional = rest[0]
	}

	cfg, path, err := config.Load(a.opts.config)
	if err != nil {
		return a.out.fail(err)
	}
	token := firstNonEmpty(a.opts.token, os.Getenv("LEWNOTES_TOKEN"))
	if *tokenStdin {
		if token != "" {
			return a.out.fail(&usageError{message: "use either --token, LEWNOTES_TOKEN, or --token-stdin"})
		}
		data, readErr := io.ReadAll(io.LimitReader(os.Stdin, 64<<10))
		if readErr != nil {
			return a.out.fail(fmt.Errorf("read token from stdin: %w", readErr))
		}
		token = strings.TrimSpace(string(data))
	}
	if token != "" {
		server := firstNonEmpty(positional, a.opts.server, os.Getenv("LEWNOTES_SERVER"), cfg.Server)
		return runTokenLogin(a, cfg, path, server, token, *open && !*noOpen)
	}
	if *noOpen {
		*noBrowser = true
	}
	return runHandoffLogin(a, cfg, path, positional, *noBrowser, *open, *deviceFlag)
}

// runTokenLogin is the break-glass, CI, and agent path: a token already exists
// and only needs verifying and storing.
func runTokenLogin(a *app, cfg config.File, path, server, token string, open bool) int {
	explicitServer := firstNonEmpty(server)
	parsed, err := client.ParseToken(token)
	if err != nil {
		return a.out.fail(&usageError{message: fmt.Sprintf("device token is not valid: %v", err)})
	}
	if server == "" {
		server = parsed.ServerURL
	}
	origin, err := client.ValidateServerURL(server)
	if err != nil {
		return a.out.fail(&usageError{message: err.Error()})
	}
	if parsed.ServerURL != origin && explicitServer != "" && !a.out.json {
		fmt.Fprintf(os.Stderr, "lewnotes: token was issued for %s but %s was given; verifying against %s\n",
			parsed.ServerURL, origin, origin)
	}

	c := client.New(origin)
	c.Token = token
	c.HTTP.Timeout = a.opts.timeout

	ctx, cancel := contextWithTimeout(a.opts)
	defer cancel()
	health, err := c.Health(ctx)
	if err != nil {
		return a.out.fail(err)
	}
	status, err := c.AuthStatus(ctx)
	if err != nil {
		return a.out.fail(err)
	}
	if status.SetupRequired {
		return a.out.fail(fmt.Errorf("server at %s has no owner yet; finish setup at %s/setup", origin, origin))
	}
	me, err := c.Me(ctx)
	if err != nil {
		return a.out.fail(err)
	}

	deviceName := firstNonEmpty(cfg.DeviceName, hostnameOr("lewnotes-cli"))
	cfg.Server = origin
	cfg.Token = token
	cfg.DeviceName = deviceName
	if err := config.Save(path, cfg); err != nil {
		return a.out.fail(err)
	}
	if open && !a.out.json {
		openBrowser(origin + "/settings/tokens")
	}

	data := map[string]string{
		"server":        origin,
		"username":      me.Username,
		"instance_name": me.InstanceName,
		"device_name":   deviceName,
		"auth":          me.Auth,
	}
	return a.out.ok(data, func() {
		fmt.Fprintf(a.out.out, "✓ Verified %s (%s) as %q\n", origin, health.Version, me.Username)
		fmt.Fprintf(a.out.out, "✓ Stored credential in %s (0600)\n", path)
		fmt.Fprintf(a.out.out, "Try: lewnotes note list --json\n")
	})
}

// runHandoffLogin is the default path: register a request, have the owner
// approve it in the web app, and receive an encrypted one-time handoff.
func runHandoffLogin(a *app, cfg config.File, cfgPath, positional string, noBrowser, forceOpen bool, deviceFlag string) int {
	interactive := stdinIsTerminal()
	server := firstNonEmpty(positional, a.opts.server, os.Getenv("LEWNOTES_SERVER"), cfg.Server)
	if server == "" {
		if !interactive {
			return a.out.fail(&usageError{
				message: "no server configured; pass the URL or run `lewnotes login https://notes.example.com` once from an interactive terminal",
			})
		}
		fmt.Fprint(os.Stderr, "Server URL (e.g. https://notes.example.com): ")
		line, readErr := bufio.NewReader(os.Stdin).ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return a.out.fail(fmt.Errorf("read server URL: %w", readErr))
		}
		server = strings.TrimSpace(line)
	}
	origin, err := client.ValidateServerURL(server)
	if err != nil {
		return a.out.fail(&usageError{message: err.Error()})
	}

	c := client.New(origin)
	c.HTTP.Timeout = a.opts.timeout

	probeCtx, probeCancel := context.WithTimeout(context.Background(), 15*time.Second)
	health, err := c.Health(probeCtx)
	if err != nil {
		probeCancel()
		return a.out.fail(err)
	}
	status, err := c.AuthStatus(probeCtx)
	probeCancel()
	if err != nil {
		return a.out.fail(err)
	}
	if status.SetupRequired {
		return a.out.fail(fmt.Errorf("server at %s has no owner yet; finish setup at %s/setup", origin, origin))
	}

	deviceName := firstNonEmpty(deviceFlag, cfg.DeviceName, hostnameOr("lewnotes-cli"))
	key, err := client.NewLoginKey()
	if err != nil {
		return a.out.fail(err)
	}
	state, err := client.NewLoginState()
	if err != nil {
		return a.out.fail(err)
	}

	var listener net.Listener
	redirectURI := ""
	if !noBrowser && interactive {
		listener, err = net.Listen("tcp", "127.0.0.1:0")
		if err == nil {
			redirectURI = "http://" + listener.Addr().String() + "/cb"
		}
	}

	createCtx, createCancel := context.WithTimeout(context.Background(), 20*time.Second)
	created, err := c.CreateLogin(createCtx, client.CreateLoginInput{
		PublicKey:   key.Public,
		State:       state,
		DeviceName:  deviceName,
		Client:      fmt.Sprintf("lewnotes/%s (%s/%s)", version, runtime.GOOS, runtime.GOARCH),
		RedirectURI: redirectURI,
	})
	createCancel()
	if err != nil {
		if listener != nil {
			_ = listener.Close()
		}
		return a.out.fail(err)
	}

	var browserServer *http.Server
	if listener != nil {
		browserServer = serveLoopback(listener)
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = browserServer.Shutdown(shutdownCtx)
		}()
	}

	if redirectURI != "" {
		fmt.Fprintf(os.Stderr, "Opening %s\n", created.ApproveURL)
		fmt.Fprintln(os.Stderr, "Waiting for approval in your browser…  (Ctrl-C to cancel)")
		if forceOpen || interactive {
			openBrowser(created.ApproveURL)
		}
	} else {
		fmt.Fprintf(os.Stderr, "Open  %s\n", created.DeviceURL)
		fmt.Fprintf(os.Stderr, "Code  %s          (expires in %s)\n",
			created.UserCode, humanDuration(time.Until(parseTimeOr(created.ExpiresAt, 10*time.Minute))))
		fmt.Fprintln(os.Stderr, "Waiting for approval…  (Ctrl-C to cancel)")
	}

	if payload, err := waitForApproval(a, c, created, state, key, origin); err != nil {
		return a.out.fail(err)
	} else {
		if payload.DeviceName != "" {
			deviceName = payload.DeviceName
		}
		cfg.Server = origin
		cfg.Token = payload.Token
		cfg.DeviceName = deviceName
		if err := config.Save(cfgPath, cfg); err != nil {
			return a.out.fail(err)
		}
		data := map[string]string{
			"server":        origin,
			"username":      payload.Username,
			"instance_name": payload.InstanceName,
			"device_name":   deviceName,
			"token_name":    deviceName,
			"auth":          "token",
		}
		return a.out.ok(data, func() {
			fmt.Fprintf(a.out.out, "✓ Authorized %q as %s on %s\n", deviceName, payload.Username, origin)
			fmt.Fprintf(a.out.out, "✓ Saved credential to %s (0600)\n", cfgPath)
			fmt.Fprintf(a.out.out, "Server reported %s.\n", emptyOr(health.Version, "an unknown version"))
			fmt.Fprintf(a.out.out, "Try: lewnotes note list --json\n")
		})
	}
}

func waitForApproval(a *app, c *client.Client, created client.LoginCreated, state string, key *client.LoginKey, origin string) (client.HandoffPayload, error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	interval := time.Duration(created.PollAfterMS) * time.Millisecond
	if interval < 500*time.Millisecond {
		interval = time.Second
	}
	deadline := time.Now().Add(11 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			cancelCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = c.CancelLogin(cancelCtx, created.ID, state)
			cancel()
			return client.HandoffPayload{}, &loginError{code: "login_cancelled", message: "Login cancelled."}
		case <-time.After(interval):
		}
		if time.Now().After(deadline) {
			return client.HandoffPayload{}, &loginError{code: "login_expired", message: "The login request expired before it was approved."}
		}

		pollCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		poll, err := c.GetLogin(pollCtx, created.ID, state)
		cancel()
		if err != nil {
			var apiErr *client.APIError
			switch {
			case errors.As(err, &apiErr) && apiErr.Status == http.StatusGone:
				return client.HandoffPayload{}, &loginError{code: "login_consumed", message: "This login request was already used."}
			case errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound:
				return client.HandoffPayload{}, &loginError{code: "login_expired", message: "The login request is no longer valid; run `lewnotes login` again."}
			default:
				// Transient: keep polling with a slow backoff until the TTL.
				if interval < 5*time.Second {
					interval += 500 * time.Millisecond
				}
				continue
			}
		}

		switch poll.Status {
		case "pending":
			interval = 2 * time.Second
			continue
		case "approved":
			payload, err := key.Decrypt(poll.Handoff, created.ID, origin)
			if err != nil {
				return client.HandoffPayload{}, &loginError{code: "handoff_invalid", message: "The server's login handoff could not be opened."}
			}
			if payload.Server != origin {
				return client.HandoffPayload{}, &loginError{
					code:    "handoff_invalid",
					message: fmt.Sprintf("The handoff was issued for %s, not %s; refusing to store it.", payload.Server, origin),
				}
			}
			parsed, err := client.ParseToken(payload.Token)
			if err != nil || parsed.ServerURL != origin {
				return client.HandoffPayload{}, &loginError{code: "handoff_invalid", message: "The handoff contained an invalid device token."}
			}
			verify := client.New(c.BaseURL)
			verify.Token = payload.Token
			verify.HTTP.Timeout = a.opts.timeout
			verifyCtx, verifyCancel := context.WithTimeout(ctx, 20*time.Second)
			me, err := verify.Me(verifyCtx)
			verifyCancel()
			if err != nil || me.Username != payload.Username {
				return client.HandoffPayload{}, &loginError{code: "handoff_invalid", message: "The issued token did not verify against the server."}
			}
			return payload, nil
		case "denied":
			return client.HandoffPayload{}, &loginError{code: "login_denied", message: "The login request was denied in the browser."}
		case "expired":
			return client.HandoffPayload{}, &loginError{code: "login_expired", message: "The login request expired before it was approved."}
		case "cancelled":
			return client.HandoffPayload{}, &loginError{code: "login_cancelled", message: "The login request was cancelled."}
		default:
			return client.HandoffPayload{}, &loginError{code: "login_failed", message: fmt.Sprintf("Unexpected login status %q.", poll.Status)}
		}
	}
}

// serveLoopback answers the browser redirect with a small confirmation page.
// It never carries a secret: the CLI fetches the ciphertext with its state.
func serveLoopback(listener net.Listener) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/cb", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprint(w, `<!doctype html><meta charset="utf-8"><title>lewnotes</title>
<body style="font-family:system-ui,sans-serif;max-width:32rem;margin:4rem auto;padding:0 1rem">
<h1 style="font-size:1.25rem">Authenticated</h1>
<p>You can close this tab and return to the terminal.</p>`)
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "lewnotes: browser callback stopped: %v\n", err)
		}
	}()
	return srv
}

func runLogout(a *app, args []string) int {
	fs := flag.NewFlagSet("logout", flag.ContinueOnError)
	revoke := fs.Bool("revoke", false, "revoke the token server-side")
	rest, err := parseCommandFlags(fs, args)
	if err != nil {
		return a.out.fail(err)
	}
	if len(rest) != 0 {
		return a.out.fail(&usageError{message: "logout takes no positional arguments"})
	}

	cfg, path, err := config.Load(a.opts.config)
	if err != nil {
		return a.out.fail(err)
	}
	server := firstNonEmpty(a.opts.server, os.Getenv("LEWNOTES_SERVER"), cfg.Server)
	token := firstNonEmpty(a.opts.token, os.Getenv("LEWNOTES_TOKEN"), cfg.Token)
	revoked := false
	if *revoke && token != "" && server != "" {
		c := client.New(server)
		c.Token = token
		c.HTTP.Timeout = a.opts.timeout
		ctx, cancel := contextWithTimeout(a.opts)
		defer cancel()
		me, meErr := c.Me(ctx)
		switch {
		case meErr == nil && me.Token != nil:
			if revokeErr := c.RevokeToken(ctx, me.Token.ID); revokeErr != nil {
				var apiErr *client.APIError
				if !errors.As(revokeErr, &apiErr) || apiErr.Status != 404 {
					return a.out.fail(revokeErr)
				}
			}
			revoked = true
		case meErr != nil:
			var apiErr *client.APIError
			if !errors.As(meErr, &apiErr) || !apiErr.IsAuth() {
				return a.out.fail(meErr)
			}
		}
	}

	cfg.Token = ""
	if err := config.Save(path, cfg); err != nil {
		return a.out.fail(err)
	}
	data := map[string]any{"logged_out": true, "revoked": revoked}
	return a.out.ok(data, func() {
		if revoked {
			fmt.Fprintln(a.out.out, "Logged out and revoked the device token.")
		} else {
			fmt.Fprintln(a.out.out, "Logged out.")
		}
	})
}

func runWhoami(a *app) int {
	if err := a.connect(true); err != nil {
		return a.out.fail(err)
	}
	ctx, cancel := contextWithTimeout(a.opts)
	defer cancel()
	me, err := a.c.Me(ctx)
	if err != nil {
		return a.out.fail(err)
	}
	return a.out.ok(me, func() {
		via := me.Auth
		if me.Token != nil {
			via = fmt.Sprintf("token %q", me.Token.Name)
		}
		fmt.Fprintf(a.out.out, "%s via %s at %s\n", me.Username, via, a.c.BaseURL)
		if me.InstanceName != "" {
			fmt.Fprintf(a.out.out, "instance: %s\n", me.InstanceName)
		}
	})
}

func openBrowser(url string) {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", url)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		command = exec.Command("xdg-open", url)
	}
	command.Stdout, command.Stderr = io.Discard, io.Discard
	_ = command.Start()
	time.Sleep(10 * time.Millisecond)
}

func hostnameOr(fallback string) string {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		return fallback
	}
	return hostname
}

func parseTimeOr(value string, fallback time.Duration) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Now().Add(fallback)
	}
	return parsed
}

func humanDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	minutes := int(d / time.Minute)
	seconds := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}

func emptyOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
