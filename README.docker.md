# Docker Compose での開発・デバッグ

本番Cloud SQLに接続してローカルでデバッグできます。

## 前提条件

1. `firebase-service-account.json` がプロジェクトルートにあること
2. サービスアカウントにCloud SQL接続権限があること

## 起動方法

```bash
# コンテナを起動（ビルド含む）
docker-compose up -d --build

# ログを確認
docker-compose logs -f app

# アプリケーションだけのログ
docker-compose logs -f app

# Cloud SQL Proxyのログ
docker-compose logs -f cloudsql-proxy

# コンテナを停止
docker-compose down
```

## デバッグのヒント

### アプリケーションログの確認

```bash
docker-compose logs -f app
```

### データベースに直接接続

Cloud SQL Proxyがポート5432で起動しているので、ローカルから接続可能：

```bash
psql -h localhost -p 5432 -U postgres -d phox-customer
# パスワード: phox-secure-password-2024
```

### コンテナ内に入る

```bash
docker-compose exec app sh
```

### イメージの再ビルド

コードを変更した場合:

```bash
docker-compose up -d --build
```

### 特定のサービスだけ再起動

```bash
docker-compose restart app
```

## トラブルシューティング

### Cloud SQL Proxyが起動しない

```bash
# サービスアカウントの権限を確認
gcloud projects get-iam-policy phoxtrot --flatten="bindings[].members" --filter="bindings.members:serviceAccount:*"

# firebase-service-account.jsonが正しいか確認
cat firebase-service-account.json | jq .project_id
```

### アプリがDBに接続できない

```bash
# Cloud SQL Proxyのログを確認
docker-compose logs cloudsql-proxy

# アプリのログを確認
docker-compose logs app
```
