//go:build !no_redis

package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/mrjvadi/tgcreator/internal/engine"
	"github.com/mrjvadi/tgcreator/internal/tmpl"
	"github.com/mrjvadi/tgcreator/internal/workflow"
)

func requireRedis(map[string]any) []string { return []string{"redis"} }

func init() {
	pKey := engine.Param{Name: "key", Label: "کلید", Type: "text", Required: true, Placeholder: "lock:{{ chat.id }}"}
	pTTL := engine.Param{Name: "ttl", Label: "انقضا", Type: "duration", Placeholder: "10m", Help: "ثانیه یا 10m"}
	engine.Describe("redis.get", engine.Meta{Label: "Redis: خواندن", Category: "redis", Icon: "R",
		Params: []engine.Param{pKey, {Name: "json", Label: "تبدیل از JSON", Type: "bool"}}})
	engine.Describe("redis.set", engine.Meta{Label: "Redis: نوشتن", Category: "redis", Icon: "R",
		Params: []engine.Param{pKey, {Name: "value", Label: "مقدار", Type: "text", Default: "1"}, pTTL, {Name: "nx", Label: "فقط اگر وجود ندارد", Type: "bool"}}})
	engine.Describe("redis.del", engine.Meta{Label: "Redis: حذف", Category: "redis", Icon: "R", Params: []engine.Param{pKey}})
	engine.Describe("redis.incr", engine.Meta{Label: "Redis: شمارنده", Category: "redis", Icon: "R",
		Params: []engine.Param{pKey, {Name: "by", Label: "افزایش", Type: "number", Default: 1}, pTTL}})
	engine.Describe("redis.command", engine.Meta{Label: "Redis: دستور دلخواه", Category: "redis", Icon: "R",
		Params: []engine.Param{{Name: "command", Label: "دستور", Type: "json", Required: true, Default: []any{"HSET", "key", "field", "value"}}}})

	engine.RegisterService("redis", openRedis)
	engine.RegisterStateBackend("redis", []string{"redis"}, func(e *engine.Engine) (engine.StateStore, error) {
		return &redisState{c: e.Service("redis").(*redis.Client), ttl: e.StateTTL()}, nil
	})

	engine.Register(engine.NodeType{
		Name:        "redis.command",
		Description: "Run any Redis command. Params: command ([\"HSET\", \"k\", \"f\", \"v\"]).",
		Requires:    requireRedis,
		New: func(b *engine.Build, n workflow.Node) (any, error) {
			c, err := b.Param(n, "command", nil)
			if err != nil {
				return nil, err
			}
			return &redisNode{fn: func(x *engine.Exec, r *redis.Client) (any, error) {
				v, err := x.Eval(c)
				if err != nil {
					return nil, err
				}
				in, _ := v.([]any)
				if len(in) == 0 {
					return nil, errors.New("command must be a non-empty list")
				}
				args := make([]any, len(in))
				for i, a := range in {
					args[i] = tmpl.ToString(a)
				}
				res, err := r.Do(x.Ctx, args...).Result()
				if errors.Is(err, redis.Nil) {
					return nil, nil
				}
				return res, err
			}}, nil
		},
	})
	engine.Register(engine.NodeType{
		Name:        "redis.get",
		Description: "GET key. Output: value or null. Params: key, json (decode value).",
		Requires:    requireRedis,
		New: func(b *engine.Build, n workflow.Node) (any, error) {
			key, err := b.Param(n, "key", "")
			if err != nil {
				return nil, err
			}
			decode, _ := n.Params["json"].(bool)
			return &redisNode{fn: func(x *engine.Exec, r *redis.Client) (any, error) {
				k, err := x.String(key)
				if err != nil {
					return nil, err
				}
				s, err := r.Get(x.Ctx, k).Result()
				if errors.Is(err, redis.Nil) {
					return nil, nil
				}
				if err != nil || !decode {
					return s, err
				}
				var v any
				return v, json.Unmarshal([]byte(s), &v)
			}}, nil
		},
	})
	engine.Register(engine.NodeType{
		Name:        "redis.set",
		Description: "SET key value. Params: key, value (objects stored as JSON), ttl (\"10m\" or seconds), nx (only if absent).",
		Requires:    requireRedis,
		New: func(b *engine.Build, n workflow.Node) (any, error) {
			key, err := b.Param(n, "key", "")
			if err != nil {
				return nil, err
			}
			val, err := b.Param(n, "value", "1")
			if err != nil {
				return nil, err
			}
			ttl, err := b.Param(n, "ttl", nil)
			if err != nil {
				return nil, err
			}
			nx, _ := n.Params["nx"].(bool)
			return &redisNode{fn: func(x *engine.Exec, r *redis.Client) (any, error) {
				k, err := x.String(key)
				if err != nil {
					return nil, err
				}
				v, err := x.Eval(val)
				if err != nil {
					return nil, err
				}
				d, err := evalDuration(x, ttl)
				if err != nil {
					return nil, err
				}
				s := redisValue(v)
				if nx {
					return r.SetNX(x.Ctx, k, s, d).Result()
				}
				return true, r.Set(x.Ctx, k, s, d).Err()
			}}, nil
		},
	})
	engine.Register(engine.NodeType{
		Name:        "redis.del",
		Description: "DEL keys. Params: key or keys.",
		Requires:    requireRedis,
		New: func(b *engine.Build, n workflow.Node) (any, error) {
			keys, err := b.Compile(paramOr(n, "keys", paramOr(n, "key", nil)))
			if err != nil {
				return nil, err
			}
			return &redisNode{fn: func(x *engine.Exec, r *redis.Client) (any, error) {
				v, err := x.Eval(keys)
				if err != nil {
					return nil, err
				}
				ks := stringList(v)
				if len(ks) == 0 {
					return int64(0), nil
				}
				return r.Del(x.Ctx, ks...).Result()
			}}, nil
		},
	})
	engine.Register(engine.NodeType{
		Name:        "redis.incr",
		Description: "INCRBY key by (default 1); ttl set on first increment. Useful for rate limits and anti-flood.",
		Requires:    requireRedis,
		New: func(b *engine.Build, n workflow.Node) (any, error) {
			key, err := b.Param(n, "key", "")
			if err != nil {
				return nil, err
			}
			by, err := b.Param(n, "by", 1)
			if err != nil {
				return nil, err
			}
			ttl, err := b.Param(n, "ttl", nil)
			if err != nil {
				return nil, err
			}
			return &redisNode{fn: func(x *engine.Exec, r *redis.Client) (any, error) {
				k, err := x.String(key)
				if err != nil {
					return nil, err
				}
				bv, err := x.Eval(by)
				if err != nil {
					return nil, err
				}
				inc, _ := tmpl.ToInt64(bv)
				d, err := evalDuration(x, ttl)
				if err != nil {
					return nil, err
				}
				n, err := r.IncrBy(x.Ctx, k, inc).Result()
				if err == nil && d > 0 && n == inc {
					err = r.Expire(x.Ctx, k, d).Err()
				}
				return n, err
			}}, nil
		},
	})
}

type redisNode struct {
	c  *redis.Client
	fn func(x *engine.Exec, r *redis.Client) (any, error)
}

func (n *redisNode) Init(e *engine.Engine) error {
	c, ok := e.Service("redis").(*redis.Client)
	if !ok {
		return errors.New("redis service not available")
	}
	n.c = c
	return nil
}

func (n *redisNode) Exec(x *engine.Exec) (engine.Result, error) {
	v, err := n.fn(x, n.c)
	if err != nil {
		return engine.Result{}, err
	}
	return engine.Result{Output: engine.Main, Data: v}, nil
}

func openRedis(ctx context.Context, e *engine.Engine, _ string) (any, io.Closer, error) {
	cfg := e.WF.Services.Redis
	if cfg == nil || cfg.URL == "" {
		return nil, nil, errors.New("services.redis.url is not set (or TGC_REDIS_URL)")
	}
	opt, err := redis.ParseURL(cfg.URL)
	if err != nil {
		return nil, nil, err
	}
	c := redis.NewClient(opt)
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := c.Ping(pctx).Err(); err != nil {
		_ = c.Close()
		return nil, nil, err
	}
	return c, c, nil
}

func redisValue(v any) any {
	switch t := v.(type) {
	case map[string]any, []any:
		b, _ := json.Marshal(t)
		return string(b)
	case nil:
		return ""
	case string:
		return t
	default:
		return tmpl.ToString(t)
	}
}

func evalDuration(x *engine.Exec, v tmpl.Value) (time.Duration, error) {
	r, err := x.Eval(v)
	if err != nil || r == nil {
		return 0, err
	}
	if n, ok := tmpl.ToInt64(r); ok {
		return time.Duration(n) * time.Second, nil
	}
	d, err := time.ParseDuration(tmpl.ToString(r))
	if err != nil {
		return 0, fmt.Errorf("bad duration %v: %w", r, err)
	}
	return d, nil
}

type redisState struct {
	c   *redis.Client
	ttl time.Duration
}

const statePrefix = "tgc:state:"

func (s *redisState) Get(ctx context.Context, key string) (map[string]any, error) {
	b, err := s.c.Get(ctx, statePrefix+key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]any
	return m, json.Unmarshal(b, &m)
}

func (s *redisState) Set(ctx context.Context, key string, v map[string]any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.c.Set(ctx, statePrefix+key, b, s.ttl).Err()
}

func (s *redisState) Delete(ctx context.Context, key string) error {
	return s.c.Del(ctx, statePrefix+key).Err()
}
