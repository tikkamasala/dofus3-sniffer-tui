package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"sniffer-tui/internal/capture"
	"sniffer-tui/internal/config"
	"sniffer-tui/internal/protoreg"
	"sniffer-tui/internal/tui"
	"sniffer-tui/internal/watch"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	log.SetFlags(0)
	cfg, err := config.Load()
	if err != nil {
		log.Printf("warning: loading config: %v", err)
	}

	connReg := protoreg.New(cfg.Connection.EnvelopeMessageName)
	gameReg := protoreg.New(cfg.Game.EnvelopeMessageName)
	mappings := protoreg.NewMappings()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var program *tea.Program

	watcher, err := watch.New(func(k watch.Kind) {
		if program != nil {
			program.Send(tui.ReloadMsg{Kind: k})
		}
	})
	if err != nil {
		return fmt.Errorf("watcher: %w", err)
	}
	defer watcher.Close()

	var (
		sessionMu     sync.Mutex
		session       *capture.Session
		sessionCancel context.CancelFunc
	)

	startCapture := func() error {
		sessionMu.Lock()
		defer sessionMu.Unlock()
		if session != nil {
			sessionCancel()
			session.Stop()
			session = nil
		}
		if cfg.Device == "" || cfg.ServerPort == 0 {
			return nil
		}

		connIPs := resolveIPs(cfg.ConnectionServer)
		knownGameIPs := parseIPs(cfg.GameServerIPs)

		onNewGameIP := func(ip net.IP) {
			if program != nil {
				program.Send(tui.NewGameIPMsg{IP: ip.String()})
			}
		}

		classifier := capture.NewClassifier(connIPs, knownGameIPs, connReg, gameReg, onNewGameIP)

		knownIPs := append([]net.IP(nil), connIPs...)
		knownIPs = append(knownIPs, knownGameIPs...)

		sCtx, sCancel := context.WithCancel(ctx)
		sess, err := capture.Start(sCtx, capture.Options{
			Device:     cfg.Device,
			ServerPort: cfg.ServerPort,
			Classifier: classifier,
			KnownIPs:   knownIPs,
		})
		if err != nil {
			sCancel()
			return err
		}
		session = sess
		sessionCancel = sCancel
		if program != nil {
			tui.StartCaptureBridge(sCtx, program, sess.Messages())
			tui.StartDropTicker(sCtx, program, sess.Dropped)
		}
		return nil
	}
	defer func() {
		sessionMu.Lock()
		defer sessionMu.Unlock()
		if session != nil {
			sessionCancel()
			session.Stop()
		}
	}()

	deps := tui.Deps{
		Cfg:          &cfg,
		ConnReg:      connReg,
		GameReg:      gameReg,
		Mappings:     mappings,
		Watcher:      watcher,
		StartCapture: startCapture,
		Dropped: func() uint64 {
			sessionMu.Lock()
			defer sessionMu.Unlock()
			if session == nil {
				return 0
			}
			return session.Dropped()
		},
		BytesReceived: func() uint64 {
			sessionMu.Lock()
			defer sessionMu.Unlock()
			if session == nil {
				return 0
			}
			return session.BytesReceived()
		},
	}

	app := tui.NewApp(deps)
	program = tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())

	if cfg.Device != "" && cfg.ServerPort != 0 {
		if err := startCapture(); err != nil {
			log.Printf("warning: auto-start capture: %v", err)
		}
	}

	_, err = program.Run()
	return err
}

// resolveIPs accepts either a literal IP or a hostname and returns all IPs
// the hostname resolves to. Errors and empty hosts yield an empty slice — the
// classifier simply won't recognise connection-server flows, which is a
// recoverable state the user can fix from the Settings tab.
func resolveIPs(host string) []net.IP {
	host = trim(host)
	if host == "" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		log.Printf("warning: resolving %q: %v", host, err)
		return nil
	}
	return ips
}

func parseIPs(list []string) []net.IP {
	out := make([]net.IP, 0, len(list))
	for _, s := range list {
		if ip := net.ParseIP(trim(s)); ip != nil {
			out = append(out, ip)
		}
	}
	return out
}

func trim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\r' || s[0] == '\n') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\r' || s[len(s)-1] == '\n') {
		s = s[:len(s)-1]
	}
	return s
}
