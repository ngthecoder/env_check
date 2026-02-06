.PHONY: build run install clean dev docker docker-run

BINARY    := envcheck
PORT      := 8484
INTERVAL  := 30s

# ───── Build ─────
build:
	go build -o $(BINARY) ./cmd/main.go

# ───── Run ─────
run: build
	./$(BINARY) --port $(PORT) --interval $(INTERVAL)

dev: build
	./$(BINARY) --port $(PORT) --interval 5s

# One-shot JSON output
json: build
	./$(BINARY) --json

# ───── Install ─────
install: build
	sudo cp $(BINARY) /usr/local/bin/
	@echo "✅ Installed to /usr/local/bin/$(BINARY)"

# Install as systemd user service
install-service: install
	mkdir -p ~/.config/systemd/user
	cp envcheck@.service ~/.config/systemd/user/envcheck.service
	sed -i "s|%h|$$HOME|g; s|%i|$$USER|g" ~/.config/systemd/user/envcheck.service
	systemctl --user daemon-reload
	systemctl --user enable envcheck
	systemctl --user start envcheck
	@echo "✅ Service installed and started"
	@echo "   Dashboard: http://localhost:$(PORT)"
	@echo "   Logs: journalctl --user -u envcheck -f"

# ───── Docker ─────
docker:
	docker build -t envcheck .

docker-run: docker
	docker run -p $(PORT):$(PORT) \
		-v $$HOME:$$HOME:ro \
		-e HOME=$$HOME \
		envcheck

# ───── Clean ─────
clean:
	rm -f $(BINARY)
