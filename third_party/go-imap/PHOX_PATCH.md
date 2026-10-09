# go-imap v2.0.0-beta.8 (patched copy)

Upstream: github.com/emersion/go-imap/v2 v2.0.0-beta.8 (cmd/ and imapserver-based client tests removed).

Patch: `imapclient/search.go` writes the SEARCH date keys
(ON/SINCE/BEFORE/SENTON/SENTSINCE/SENTBEFORE) as an atom (`SINCE 4-Oct-2026`)
instead of a quoted string (`SINCE "4-Oct-2026"`). Both are valid per RFC 3501,
but Apache James 3.9 answers the quoted form with
`BAD UID failed. Illegal arguments.`, which stopped every phox IMAP worker
after the 2026-10 mail server cut-over to James.

Drop this directory and the `replace` in go.mod once upstream emits atoms
(or James accepts quoted dates).
