# phox-customer

Phox CRM のバックエンド。Go + Connect-Go + PostgreSQL + SQLC。認証は Keycloak (OIDC)。

## 機能

- Keycloak 発行の JWT を JWKS 方式で検証 (`lestrrat-go/jwx`)
- Keycloak Admin API 経由でのユーザー作成 (`Nerzal/gocloak`)
- Connect-Go による gRPC 互換 HTTP/2 API
- PostgreSQL + SQLC による型安全なクエリ
- インターセプターベースの認証・認可

## 認証

全エンドポイントは Connect インターセプター (`internal/service/auth/interceptor.go`) で
`Authorization: Bearer <access_token>` を要求する。トークンは Keycloak の
`phox-ui` クライアントから発行された access token を前提とし、JWKS で署名検証し、
`iss` / `aud` / `exp` を検証する。検証成功後、`token.Subject()` (Keycloak の `sub`) を
アプリ側 `User.id` の主キーとして DB を照会する。

### 設定 (`app.env`)

```env
JWT_ENABLED=true
JWT_PROJECT_ID=phox-customer                                                 # 期待される aud クレーム
JWT_ISSUER_URL=http://localhost:8080/realms/phox                             # トークンの iss と完全一致させる
JWT_JWKS_URL=http://localhost:8080/realms/phox/protocol/openid-connect/certs # backend が実際にフェッチする URL

KEYCLOAK_URL=http://localhost:8080
KEYCLOAK_REALM=phox
KEYCLOAK_ADMIN_CLIENT_ID=phox-admin-cli
KEYCLOAK_ADMIN_CLIENT_SECRET=...
```

**重要**: docker-compose 配下で動かす場合、`JWT_ISSUER_URL` はブラウザが見るホスト名
(`localhost:8080`) と一致させ、`JWT_JWKS_URL` はコンテナ内から解決できるホスト名
(`keycloak:8080`) に切り替える。iss は文字列照合だが JWKS は実アクセスするため。

### Audience マッパー

Keycloak 標準では access token の `aud` は `"account"` になる。phox-customer が
`aud=phox-customer` を期待するので、`phox-ui` クライアントに
`oidc-audience-mapper` を仕込んで `phox-customer` を `aud` 配列に追加する。
この設定は `phox-manifest/keycloak/realm-phox.json` に同梱されている。

## MCP サーバー (Phase 24)

`/mcp` に MCP (Model Context Protocol) サーバーを Streamable HTTP で公開している。
Claude Code / Claude Desktop などの MCP クライアントから Phox CRM を読める:

| tool | 内容 |
|---|---|
| `list_books` | アクセス可能な顧客リスト一覧 |
| `search_customers` | 顧客全文検索 (ES / kuromoji、都道府県フィルタ付き) |
| `get_customer` | 顧客 1 件の詳細 |
| `list_customer_activities` | 顧客単位の活動履歴 |
| `list_book_activities` | Book 横断の活動フィード (種別/担当者/期間フィルタ) |
| `get_call_stats` / `get_mail_stats` | 担当者別のコール/メール集計 |

設計:

- **読み取り専用** (v1)。書き込み tool は必要になったら追加する。
- 認証は Connect RPC と完全に同一 — `Authorization: Bearer <Keycloak JWT>`
  (aud=phox-customer) を必須にし、`auth.Interceptor.Authenticate` を共用。
  tool は既存 service を in-process で呼ぶので Permit / role チェックも
  そのまま効く。認可の実装が二重化しない。
- transport は **Stateless + JSONResponse** — セッション親和性が不要なので
  replicas > 1 でも安全、curl でも叩ける。
- `MCP_ENABLED=false` でエンドポイントごと無効化 (既定 true)。

Claude Code からの接続例 (staging):

```bash
TOKEN=$(curl -s https://auth.0utl1er.tech/realms/company/protocol/openid-connect/token \
  -d grant_type=password -d client_id=phox-mcp \
  -d client_secret=$PHOX_MCP_CLIENT_SECRET \
  -d username=e2e-bot -d password=$E2E_PW | jq -r .access_token)

claude mcp add --transport http phox https://phox-api-staging.0utl1er.tech/mcp \
  --header "Authorization: Bearer $TOKEN"
```

(`phox-mcp` は password grant 用の confidential client。kuki-dc の
`phox/bootstrap/keycloak-mcp-client.sh` で作成する。token は realm 設定の
lifetime で失効する点に注意 — 長時間使うなら都度取得すること。)

実装: `internal/mcpserver/` (tool 定義とテストは同 package)。

## ローカル開発

```bash
# 1. Keycloak + postgres を起動 (phox-manifest)
cd ../phox-manifest && docker compose up -d

# 2. DB マイグレーション + シード
cd ../phox-customer && make migrateup

# 3. backend 起動
go run main.go
```

## ディレクトリ

```
├── db/
│   ├── migration/       # SQL マイグレーション (000001_seed_initial_data で Keycloak UUID の User を投入)
│   └── query/           # SQLC クエリ定義
├── gen/
│   ├── pb/              # Protobuf 生成
│   └── sqlc/            # SQLC 生成
├── internal/
│   ├── keycloakadmin/   # Keycloak Admin API ラッパー (gocloak)
│   ├── service/
│   │   ├── auth/        # Connect インターセプター (JWT 検証)
│   │   ├── book/
│   │   ├── customer/
│   │   └── user/        # CreateCompanyUser 等
│   └── util/            # Config (viper)
├── proto/
└── main.go
```
