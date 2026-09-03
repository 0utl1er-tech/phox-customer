package campaign

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/rs/zerolog/log"
)

// Phase 27f: メール健全性チェック。
//
// バウンス率が上がると送信ドメインの評価が落ち、以後の全メールがスパム判定
// される。ここでは「送らないほうがいい宛先を送る前に落とす」ための検証と、
// 「送信元ドメインが正しく設定されているか」の点検を提供する。

const (
	// domainCacheTTL は MX 検証キャッシュの有効期限。DNS の変化はそこまで
	// 速くないので長め (ドメイン消滅の検出が遅れても、実害はバウンス 1 通)。
	domainCacheTTL = 30 * 24 * time.Hour
	// dnsTimeout は 1 回の DNS 問い合わせのタイムアウト。
	dnsTimeout = 5 * time.Second
	// mxLookupConcurrency はキャンペーン作成時の並列 lookup 数。
	mxLookupConcurrency = 8
)

// roleAddressLocals は「担当者個人ではない」ローカルパート。コールドメールで
// 送ると苦情率が上がりやすく、返信も期待できないため作成時に警告する
// (除外はしない — 小規模事業者では info@ が実質的な代表窓口のこともある)。
var roleAddressLocals = map[string]bool{
	"info": true, "admin": true, "support": true, "sales": true,
	"contact": true, "office": true, "webmaster": true, "postmaster": true,
	"noreply": true, "no-reply": true, "donotreply": true, "abuse": true,
	"help": true, "inquiry": true, "mail": true,
}

// IsRoleAddress は role-based アドレス (info@ 等) かどうかを返す。
func IsRoleAddress(email string) bool {
	local, _, ok := splitEmail(email)
	if !ok {
		return false
	}
	// +suffix や . 区切りは無視して素のローカルパートで判定
	if i := strings.IndexAny(local, "+"); i >= 0 {
		local = local[:i]
	}
	return roleAddressLocals[local]
}

// splitEmail は email を local/domain に分ける (小文字化済み)。
func splitEmail(email string) (local, domain string, ok bool) {
	e := strings.ToLower(strings.TrimSpace(email))
	i := strings.LastIndex(e, "@")
	if i <= 0 || i == len(e)-1 {
		return "", "", false
	}
	local, domain = e[:i], e[i+1:]
	if !strings.Contains(domain, ".") || strings.Contains(domain, " ") {
		return "", "", false
	}
	return local, domain, true
}

// DomainOf は email のドメイン部を返す (不正なら空)。
func DomainOf(email string) string {
	_, d, ok := splitEmail(email)
	if !ok {
		return ""
	}
	return d
}

// MXChecker は宛先ドメインの MX レコード有無を検証する。結果は DomainHealth
// テーブルにキャッシュし、DNS を受信者ごとに引かない。
//
// 「MX が無い = そのドメインはメールを受け取れない」ので、送ればほぼ確実に
// ハードバウンスになる。送る前に落とすことでバウンス率を汚さない。
// DNS 障害など判定不能なときは送信対象に残す (安全側 = 送る)。
type MXChecker struct {
	queries  *db.Queries
	resolver *net.Resolver
}

func NewMXChecker(queries *db.Queries) *MXChecker {
	return &MXChecker{queries: queries, resolver: net.DefaultResolver}
}

// MXResult は 1 ドメインの検証結果。
type MXResult struct {
	HasMX bool
	Host  string
	// Unknown は DNS 障害等で判定できなかったことを示す。呼び出し側は
	// これを「配信可能」として扱う (誤って除外しないため)。
	Unknown bool
}

// Check は 1 ドメインを検証する (キャッシュ優先)。
func (m *MXChecker) Check(ctx context.Context, domain string) MXResult {
	if m == nil || domain == "" {
		return MXResult{HasMX: true, Unknown: true}
	}
	domain = strings.ToLower(domain)
	if row, err := m.queries.GetDomainHealth(ctx, domain); err == nil {
		if time.Since(row.CheckedAt) < domainCacheTTL {
			return MXResult{HasMX: row.HasMx, Host: row.MxHost}
		}
	}
	return m.lookupAndStore(ctx, domain)
}

func (m *MXChecker) lookupAndStore(ctx context.Context, domain string) MXResult {
	lookupCtx, cancel := context.WithTimeout(ctx, dnsTimeout)
	defer cancel()

	mxs, err := m.resolver.LookupMX(lookupCtx, domain)
	res := MXResult{}
	switch {
	case err == nil && len(mxs) > 0:
		res.HasMX, res.Host = true, strings.TrimSuffix(mxs[0].Host, ".")
	case isDNSNotFound(err):
		// ドメインが存在しない / MX が無い。A レコードへのフォールバック
		// (RFC 5321 implicit MX) を確認してから配信不能と判断する。
		if addrs, aerr := m.resolver.LookupHost(lookupCtx, domain); aerr == nil && len(addrs) > 0 {
			res.HasMX, res.Host = true, domain+" (implicit MX/A)"
		} else {
			res.HasMX = false
		}
	default:
		// タイムアウト・SERVFAIL 等。判定不能なのでキャッシュせず送信対象に残す。
		log.Debug().Err(err).Str("domain", domain).Msg("campaign: MX lookup inconclusive")
		return MXResult{HasMX: true, Unknown: true}
	}

	if err := m.queries.UpsertDomainHealth(ctx, db.UpsertDomainHealthParams{
		Lower:  domain,
		HasMx:  res.HasMX,
		MxHost: res.Host,
	}); err != nil {
		log.Warn().Err(err).Str("domain", domain).Msg("campaign: cache domain health failed")
	}
	return res
}

// CheckMany は複数ドメインを並列に検証する (キャンペーン作成時)。
// ctx が期限切れになったら残りは Unknown (= 配信可能扱い) で返す。
func (m *MXChecker) CheckMany(ctx context.Context, domains []string) map[string]MXResult {
	out := make(map[string]MXResult, len(domains))
	if m == nil || len(domains) == 0 {
		return out
	}

	// キャッシュ済みを先に埋める (DNS を引く数を減らす)。
	pending := make([]string, 0, len(domains))
	if rows, err := m.queries.ListFreshDomainHealth(ctx, db.ListFreshDomainHealthParams{
		Domains:    domains,
		FreshAfter: time.Now().Add(-domainCacheTTL),
	}); err == nil {
		cached := make(map[string]db.DomainHealth, len(rows))
		for _, r := range rows {
			cached[r.Domain] = r
		}
		for _, d := range domains {
			if r, ok := cached[d]; ok {
				out[d] = MXResult{HasMX: r.HasMx, Host: r.MxHost}
			} else {
				pending = append(pending, d)
			}
		}
	} else {
		pending = domains
	}

	var mu sync.Mutex
	sem := make(chan struct{}, mxLookupConcurrency)
	var wg sync.WaitGroup
	for _, d := range pending {
		if ctx.Err() != nil {
			mu.Lock()
			out[d] = MXResult{HasMX: true, Unknown: true}
			mu.Unlock()
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(domain string) {
			defer wg.Done()
			defer func() { <-sem }()
			r := m.lookupAndStore(ctx, domain)
			mu.Lock()
			out[domain] = r
			mu.Unlock()
		}(d)
	}
	wg.Wait()
	return out
}

// ─── 送信元ドメインの健全性 (SPF / DMARC / MX) ────────────────────

// SenderDomainHealth は送信元ドメインの DNS 設定の点検結果。
type SenderDomainHealth struct {
	Domain string

	HasMX  bool
	MXHost string

	HasSPF bool
	SPF    string
	// SPFSoftFail は "~all" (softfail) を使っていることを示す。
	// コールドメールでは "-all" (hardfail) のほうが評価されやすい。
	SPFSoftFail bool

	HasDMARC bool
	DMARC    string
	// DMARCPolicy は p= の値 (none/quarantine/reject)。none は「設定しただけ」。
	DMARCPolicy string
	// DMARCHasRUA は集約レポート送付先 (rua=) があるか。監視の前提。
	DMARCHasRUA bool

	// HasDKIM は selector を指定して調べたときのみ意味を持つ。
	HasDKIM    bool
	DKIMSelect string

	// Warnings は人が読める指摘。UI にそのまま出す。
	Warnings []string
}

// CheckSenderDomain は送信元ドメインの SPF/DMARC/MX (+ 任意で DKIM) を調べる。
// dkimSelector が空なら DKIM は調べない (mailu の既定は "dkim")。
func CheckSenderDomain(ctx context.Context, domain, dkimSelector string) SenderDomainHealth {
	h := SenderDomainHealth{Domain: strings.ToLower(domain)}
	if h.Domain == "" {
		return h
	}
	r := net.DefaultResolver
	lookupCtx, cancel := context.WithTimeout(ctx, dnsTimeout)
	defer cancel()

	if mxs, err := r.LookupMX(lookupCtx, h.Domain); err == nil && len(mxs) > 0 {
		h.HasMX, h.MXHost = true, strings.TrimSuffix(mxs[0].Host, ".")
	} else {
		h.Warnings = append(h.Warnings, "MX レコードがありません (返信やバウンスを受け取れません)")
	}

	// SPF は TXT の "v=spf1 ..." レコード。
	if txts, err := r.LookupTXT(lookupCtx, h.Domain); err == nil {
		for _, t := range txts {
			if strings.HasPrefix(strings.ToLower(t), "v=spf1") {
				h.HasSPF, h.SPF = true, t
				lower := strings.ToLower(t)
				h.SPFSoftFail = strings.Contains(lower, "~all")
				if !strings.Contains(lower, "all") {
					h.Warnings = append(h.Warnings, "SPF に all メカニズムがありません (評価が不安定になります)")
				} else if h.SPFSoftFail {
					h.Warnings = append(h.Warnings, "SPF が ~all (softfail) です。なりすまし対策としては -all が強く推奨されます")
				}
				break
			}
		}
	}
	if !h.HasSPF {
		h.Warnings = append(h.Warnings, "SPF レコードがありません (迷惑メール判定される主要因です)")
	}

	// DMARC は _dmarc.<domain> の TXT。
	if txts, err := r.LookupTXT(lookupCtx, "_dmarc."+h.Domain); err == nil {
		for _, t := range txts {
			if strings.HasPrefix(strings.ToLower(t), "v=dmarc1") {
				h.HasDMARC, h.DMARC = true, t
				for _, part := range strings.Split(t, ";") {
					kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
					if len(kv) != 2 {
						continue
					}
					switch strings.ToLower(strings.TrimSpace(kv[0])) {
					case "p":
						h.DMARCPolicy = strings.ToLower(strings.TrimSpace(kv[1]))
					case "rua":
						h.DMARCHasRUA = true
					}
				}
				break
			}
		}
	}
	switch {
	case !h.HasDMARC:
		h.Warnings = append(h.Warnings, "DMARC レコードがありません (Gmail/Yahoo の一括送信者要件を満たしません)")
	case h.DMARCPolicy == "none":
		h.Warnings = append(h.Warnings, "DMARC ポリシーが p=none です (監視のみ。将来 quarantine 以上に上げてください)")
	}
	if h.HasDMARC && !h.DMARCHasRUA {
		h.Warnings = append(h.Warnings, "DMARC に rua= がありません (集約レポートを受け取れず異常に気付けません)")
	}

	if dkimSelector != "" {
		h.DKIMSelect = dkimSelector
		if txts, err := r.LookupTXT(lookupCtx, dkimSelector+"._domainkey."+h.Domain); err == nil {
			for _, t := range txts {
				if strings.Contains(strings.ToLower(t), "p=") {
					h.HasDKIM = true
					break
				}
			}
		}
		if !h.HasDKIM {
			h.Warnings = append(h.Warnings,
				"DKIM 公開鍵が見つかりません (selector="+dkimSelector+")。mailu の DKIM 設定を確認してください")
		}
	}
	return h
}

// isDNSNotFound は「そのドメインにレコードが無い」ことが確定したエラーか。
// タイムアウトや SERVFAIL は含まない (判定不能として扱いたいため)。
func isDNSNotFound(err error) bool {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return dnsErr.IsNotFound
	}
	return false
}
