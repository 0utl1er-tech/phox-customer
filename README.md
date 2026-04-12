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
