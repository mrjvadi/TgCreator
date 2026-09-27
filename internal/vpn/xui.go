package vpn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mrjvadi/tgcreator/internal/workflow"
	"github.com/mrjvadi/tgcreator/internal/xui"
)

// xuiPanel adapts 3x-ui and alireza0 x-ui. Users are inbound clients;
// the username is the client's email.
type xuiPanel struct {
	c       *xui.Client
	address string
}

func newXUI(cfg workflow.VPNPanel) (Panel, error) {
	if cfg.Type == "" {
		cfg.Type = "3x-ui"
	}
	c, err := xui.New(cfg)
	if err != nil {
		return nil, err
	}
	addr := cfg.Address
	if addr == "" {
		addr = c.Host()
	}
	return &xuiPanel{c: c, address: addr}, nil
}

func (p *xuiPanel) Type() string { return p.c.Type() }

func xuiExpiry(ms int64) Expiry {
	switch {
	case ms > 0:
		return Expiry{Kind: At, At: time.UnixMilli(ms)}
	case ms < 0:
		return Expiry{Kind: Pending, Duration: time.Duration(-ms) * time.Millisecond}
	}
	return Expiry{Kind: Never}
}

func xuiExpiryMs(e Expiry) int64 {
	switch e.Kind {
	case At:
		return e.At.UnixMilli()
	case Pending:
		return -e.Duration.Milliseconds()
	}
	return 0
}

func (p *xuiPanel) account(ctx context.Context, f *xui.Found, sub string, now time.Time) *Account {
	c, in, t := f.Client, f.Inbound, f.Traffic
	total := int64(num(c["totalGB"]))
	if t.Total != 0 {
		total = t.Total
	}
	expMs := int64(num(c["expiryTime"]))
	if t.ExpiryTime != 0 {
		expMs = t.ExpiryTime // the panel starts first-use timers here
	}
	enabled, _ := c["enable"].(bool)
	a := &Account{
		Username: str(c["email"]),
		ID:       str(c[xui.ClientKey(in.Protocol)]),
		Enabled:  enabled,
		Used:     t.Up + t.Down,
		Total:    total,
		Expire:   xuiExpiry(expMs),
		SubLink:  xui.SubLink(sub, str(c["subId"])),
		Links:    xui.Links(in, c, p.address),
		Note:     str(c["comment"]),
		TgID:     str(c["tgId"]),
		LimitIP:  int(num(c["limitIp"])),
		Groups:   []string{strconv.Itoa(in.ID)},
		Protocol: in.Protocol,
	}
	if a.TgID == "0" {
		a.TgID = ""
	}
	if t.LastOnline > 0 {
		a.OnlineAt = time.UnixMilli(t.LastOnline)
	}
	// X-UI disables clients itself when they run out: tell the reasons apart.
	switch {
	case a.Expire.Kind == At && a.Expire.At.Before(now):
		a.Status = "expired"
	case a.Total > 0 && a.Used >= a.Total:
		a.Status = "limited"
	case !enabled:
		a.Status = "disabled"
	default:
		a.Status = "active"
	}
	return a
}

func (p *xuiPanel) Create(ctx context.Context, s Spec) (*Account, error) {
	if len(s.Groups) == 0 || s.Groups[0] == "" {
		return nil, errors.New("x-ui: the inbound id is required")
	}
	id, err := strconv.Atoi(strings.TrimSpace(s.Groups[0]))
	if err != nil {
		return nil, fmt.Errorf("x-ui: inbound id %q is not a number", s.Groups[0])
	}
	in, err := p.c.Inbound(ctx, id)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	client := xui.NewClient(in, xui.Spec{
		Email: s.Username, TotalBytes: s.TotalBytes, Days: s.Days, AfterFirstUse: s.AfterFirstUse,
		LimitIP: s.LimitIP, Flow: s.Flow, TgID: s.TgID, Comment: s.Note, SubID: s.SubID, Disabled: s.Disabled,
	}, p.c.Type(), now)
	if err := p.c.AddClient(ctx, in.ID, client); err != nil {
		if errors.Is(err, xui.ErrExists) {
			return nil, ErrExists
		}
		return nil, err
	}
	f := &xui.Found{Inbound: in, Client: client, Traffic: xui.Traffic{InboundID: in.ID, Email: s.Username, Enable: !s.Disabled}}
	return p.account(ctx, f, p.c.SubBase(ctx), now), nil
}

func (p *xuiPanel) find(ctx context.Context, username string) (*xui.Found, error) {
	f, err := p.c.FindClient(ctx, username)
	if errors.Is(err, xui.ErrNotFound) {
		return nil, ErrNotFound
	}
	return f, err
}

func (p *xuiPanel) Get(ctx context.Context, username string) (*Account, error) {
	f, err := p.find(ctx, username)
	if err != nil {
		return nil, err
	}
	return p.account(ctx, f, p.c.SubBase(ctx), time.Now()), nil
}

func (p *xuiPanel) Update(ctx context.Context, username string, ch Change) (*Account, error) {
	f, err := p.find(ctx, username)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	a := p.account(ctx, f, "", now)
	renewed := ch.Apply(a, now)
	cl := make(map[string]any, len(f.Client)+1)
	for k, v := range f.Client {
		cl[k] = v
	}
	cl["totalGB"], cl["expiryTime"] = a.Total, xuiExpiryMs(a.Expire)
	switch {
	case ch.Enable != nil:
		cl["enable"] = *ch.Enable
	case renewed:
		cl["enable"] = true // the panel had switched it off when it ran out
	}
	if ch.LimitIP != nil {
		cl["limitIp"] = *ch.LimitIP
	}
	if ch.Note != nil {
		cl["comment"] = *ch.Note
	}
	cl["updated_at"] = now.UnixMilli()
	if err := p.c.UpdateClient(ctx, f, cl); err != nil {
		return nil, err
	}
	if ch.ResetUsage {
		if err := p.c.ResetTraffic(ctx, f.Inbound.ID, username); err != nil {
			return nil, err
		}
	}
	return p.Get(ctx, username)
}

func (p *xuiPanel) Delete(ctx context.Context, username string) error {
	f, err := p.find(ctx, username)
	if err != nil {
		return err
	}
	return p.c.DeleteClient(ctx, f)
}

func (p *xuiPanel) Find(ctx context.Context, q Query) ([]*Account, error) {
	ins, err := p.c.Inbounds(ctx)
	if err != nil {
		return nil, err
	}
	sub, now := p.c.SubBase(ctx), time.Now()
	var out []*Account
	for i := range ins {
		in := &ins[i]
		clients, err := in.Clients()
		if err != nil {
			continue
		}
		for _, cl := range clients {
			if q.TgID != "" && str(cl["tgId"]) != q.TgID {
				continue // skip building links for users that cannot match
			}
			st, _ := in.Stat(str(cl["email"]))
			if a := p.account(ctx, &xui.Found{Inbound: in, Client: cl, Traffic: st}, sub, now); q.Match(a) {
				out = append(out, a)
			}
		}
	}
	return out, nil
}

func (p *xuiPanel) Onlines(ctx context.Context) ([]string, error) { return p.c.Onlines(ctx) }

func (p *xuiPanel) Groups(ctx context.Context) ([]Group, error) {
	ins, err := p.c.Inbounds(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Group, len(ins))
	for i := range ins {
		cl, _ := ins[i].Clients()
		out[i] = Group{ID: strconv.Itoa(ins[i].ID), Name: ins[i].Remark, Protocol: ins[i].Protocol, Port: ins[i].Port, Users: len(cl), Enabled: ins[i].Enable}
	}
	return out, nil
}

func (p *xuiPanel) Call(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	return p.c.Call(ctx, method, "/"+strings.TrimLeft(path, "/"), body)
}
