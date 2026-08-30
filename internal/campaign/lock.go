package campaign

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// leaderLock は Redis ベースの単純なリーダー選出。prod は backend replicas=2
// なので、送信 worker はロックを取れた pod だけが動く。ロックが破れた場合でも
// ClaimCampaignRecipient の FOR UPDATE SKIP LOCKED が二重送信を防ぐ二重防御。
//
// rdb が nil (REDIS_ADDR 未設定 = dev 単 pod) ならロックレスで常に leader。
type leaderLock struct {
	rdb *redis.Client
	key string
	val string // この pod を識別する値 (hostname + 乱数)
	ttl time.Duration
}

// renewScript: 自分の値のときだけ TTL を延長する (compare-and-expire)。
var renewScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
  return redis.call("pexpire", KEYS[1], ARGV[2])
else
  return 0
end`)

// releaseScript: 自分の値のときだけ削除する (compare-and-delete)。
var releaseScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
  return redis.call("del", KEYS[1])
else
  return 0
end`)

func newLeaderLock(rdb *redis.Client, key, val string, ttl time.Duration) *leaderLock {
	return &leaderLock{rdb: rdb, key: key, val: val, ttl: ttl}
}

// tryAcquireOrRenew は「未保持なら SET NX、保持中なら TTL 延長」を 1 回試み、
// leader かどうかを返す。
func (l *leaderLock) tryAcquireOrRenew(ctx context.Context) bool {
	if l.rdb == nil {
		return true // dev: ロックレス
	}
	ok, err := l.rdb.SetNX(ctx, l.key, l.val, l.ttl).Result()
	if err != nil {
		return false // Redis 不通時は安全側 (送信しない)
	}
	if ok {
		return true
	}
	n, err := renewScript.Run(ctx, l.rdb, []string{l.key}, l.val, l.ttl.Milliseconds()).Int()
	return err == nil && n == 1
}

func (l *leaderLock) release(ctx context.Context) {
	if l.rdb == nil {
		return
	}
	_ = releaseScript.Run(ctx, l.rdb, []string{l.key}, l.val).Err()
}
