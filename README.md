# phox-customer
phox-customer は Firebase Authentication を使用した認証機能を持つ Go バックエンドサービスです。

## 機能

- Firebase Authentication による JWT トークン検証
- Connect-Go を使用した gRPC-Web 対応 API
- PostgreSQL データベース統合
- ミドルウェアベースの認証・認可
- プロトコルバッファによる型安全な API 定義

## 認証

このアプリケーションは Firebase Authentication を使用して JWT トークンを検証します。

### 設定

`app.env` ファイルで以下の設定を行ってください：

```env
# JWT Configuration (Firebase)
JWT_ENABLED=true
JWT_CLIENT_ID=your-firebase-client-id.apps.googleusercontent.com
JWT_PROJECT_ID=your-firebase-project-id
JWT_ISSUER_URL=https://securetoken.google.com/your-firebase-project-id
```

### Firebase プロジェクトの設定

1. Firebase コンソールでプロジェクトを作成
2. Authentication を有効化
3. 必要な認証プロバイダーを設定
4. サービスアカウントキーをダウンロード
5. 環境変数 `GOOGLE_APPLICATION_CREDENTIALS` にサービスアカウントキーのパスを設定

### 使用方法

```go
// JWT検証器を作成
jwtValidator, err := NewFirebaseJWTValidator(config.JWT, &logger)
if err != nil {
    log.Fatal("Failed to create JWT validator:", err)
}

// ミドルウェアを作成
jwtMiddleware := NewJWTMiddleware(jwtValidator)

// ミドルウェアチェーンを作成
middlewareChain := Chain(loggerMiddleware, jwtMiddleware)
```

## Quick start