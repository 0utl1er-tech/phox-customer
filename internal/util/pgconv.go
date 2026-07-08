package util

import "github.com/jackc/pgx/v5/pgtype"

// OptionalText converts an optional proto string field to pgtype.Text for
// COALESCE-style partial updates.
//
// nil と空文字列はどちらも「未指定」(Valid=false) として扱う。UI は変更したい
// フィールドだけを送る前提であり、空文字列を Valid=true で通すと SQL 側の
// COALESCE($n, existing) が空文字列を正規の値とみなし、既存の DB 値を
// 黙って "" で上書きしてしまう (2026-04-21 に customer 更新で実際に発生)。
// フィールドの明示的なクリアは専用 RPC / sentinel が必要だが、現状の
// プロダクトには存在しない。
func OptionalText(p *string) pgtype.Text {
	if p == nil || *p == "" {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: *p, Valid: true}
}
