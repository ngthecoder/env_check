#!/bin/bash
# setup-repo.sh — GitHub リポジトリの作成とプッシュ
# 使い方: ./setup-repo.sh
#
# 前提:
#   - gh CLI がインストール済み (brew install gh)
#   - gh auth login 済み
#   - Go 1.22+ がインストール済み

set -euo pipefail

REPO_NAME="envcheck"

echo "🔧 Initializing Go modules..."
go mod tidy

echo "📦 Building to verify..."
go build -o envcheck ./cmd/main.go
rm -f envcheck
echo "✅ Build successful"

echo ""
echo "🚀 Creating GitHub repo..."
git init
git add .
git commit -m "init: envcheck - dev environment dashboard"

# GitHub CLIでリポ作成 + プッシュ
gh repo create "$REPO_NAME" --public --source=. --push

echo ""
echo "✅ Done!"
echo "   Repo: https://github.com/$(gh api user -q .login)/$REPO_NAME"
echo ""
echo "📝 Next: Open Claude Code Web and connect to this repo"
