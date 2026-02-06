# envcheck

Dev環境のランタイムバージョンとパッケージをリアルタイムで可視化するダッシュボード。

```
⌘ envcheck
├── Runtimes: pyenv, goenv, nvm, fnm, rbenv, jenv, rustup, asdf, mise
└── Packages: pip, npm, go modules, cargo, gem, brew
```

## クイックスタート

```bash
# ビルドして起動
make run

# → http://localhost:8484 でダッシュボードが開く
```

## 使い方

### 1. サーバーモード（推奨）

バックグラウンドで定期スキャンしつつ、ダッシュボードを配信：

```bash
# デフォルト: 30秒間隔、ポート8484
make run

# カスタム
./envcheck --port 3000 --interval 1m

# 開発モード（5秒間隔）
make dev
```

ダッシュボードは SSE (Server-Sent Events) でリアルタイム更新される。
`.python-version` や `go.mod` などのファイル変更も自動検知してリスキャン。

### 2. ワンショットモード

JSON出力だけ欲しい場合：

```bash
./envcheck --json
# or
make json
```

### 3. systemd サービス（常駐）

PCを起動したら自動で走るようにする：

```bash
make install-service

# ログ確認
journalctl --user -u envcheck -f

# 停止
systemctl --user stop envcheck
```

### 4. Docker

```bash
make docker-run

# or
docker build -t envcheck .
docker run -p 8484:8484 -v $HOME:$HOME:ro -e HOME=$HOME envcheck
```

## API

| Endpoint | Method | 説明 |
|---|---|---|
| `/` | GET | ダッシュボード |
| `/api/status` | GET | 最新のスキャン結果 (JSON) |
| `/api/rescan` | POST | 手動リスキャン |
| `/api/events` | GET | SSE リアルタイムストリーム |

## アーキテクチャ

```
cmd/
  main.go          # エントリポイント + CLI flags
  web/
    index.html     # 埋め込みダッシュボード (embed.FS)
internal/
  collector/
    collector.go   # ランタイム＆パッケージ収集
  server/
    server.go      # HTTP + SSE + ファイルウォッチャー
```

**仕組み:**
1. 起動時にフルスキャン（全*env ツール + パッケージマネージャー）
2. 設定間隔で定期リスキャン
3. バージョンファイル変更を `fsnotify` で監視 → 即座にリスキャン
4. SSE でダッシュボードにリアルタイムプッシュ
5. ダッシュボードは `go:embed` でバイナリに同梱（単一バイナリ）

## ファイル監視対象

| ファイル | 検出内容 |
|---|---|
| `.python-version` | pyenv ローカルバージョン変更 |
| `.go-version` | goenv ローカルバージョン変更 |
| `.node-version` | nvm/fnm ローカルバージョン変更 |
| `.tool-versions` | asdf/mise バージョン変更 |
| `go.mod` | Go依存関係の変更 |
| `package.json` | Node依存関係の変更 |
| `Cargo.toml` | Rust依存関係の変更 |
| `Gemfile` | Ruby依存関係の変更 |
