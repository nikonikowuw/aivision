.PHONY: all build build-ui build-backend dev-ui dev-backend test vet clean

ROOT_DIR := $(shell pwd)
UI_DIR := $(ROOT_DIR)/ui
BACKEND_DIR := $(ROOT_DIR)/argus
BIN_DIR := $(ROOT_DIR)/bin

all: build

# 完整构建：编译前端 SPA -> 同步产物到 Go 内嵌目录 -> 编译 Go 单二进制
build: build-ui build-backend
	@mkdir -p $(BIN_DIR)
	@cp -f $(BACKEND_DIR)/bin/api $(BIN_DIR)/argus
	@cp -f $(BACKEND_DIR)/bin/migrate $(BIN_DIR)/migrate 2>/dev/null || true
	@cp -f $(BACKEND_DIR)/bin/bootstrap $(BIN_DIR)/bootstrap 2>/dev/null || true
	@echo "==> Build complete! Output standalone binary at: $(BIN_DIR)/argus"

# 仅构建前端并同步至 Go embed 目录
build-ui:
	@echo "==> Building frontend UI..."
	cd $(UI_DIR) && pnpm --filter @vben/web-antd build
	@echo "==> Syncing frontend dist to backend embed directory..."
	@mkdir -p $(BACKEND_DIR)/internal/web/dist
	cp -r $(UI_DIR)/apps/web-antd/dist/* $(BACKEND_DIR)/internal/web/dist/

# 仅构建后端（若未构建前端则使用现有内嵌产物或默认占位页）
build-backend:
	@echo "==> Building backend Go binaries..."
	$(MAKE) -C $(BACKEND_DIR) build

# 本地前端开发服务（Vite HMR，代理至 :8000）
dev-ui:
	cd $(UI_DIR) && pnpm dev:antd

# 本地后端开发服务（Air 热重载）
dev-backend:
	$(MAKE) -C $(BACKEND_DIR) dev

# 运行后端测试
test:
	$(MAKE) -C $(BACKEND_DIR) test

# 代码检查
vet:
	$(MAKE) -C $(BACKEND_DIR) vet

# 清理构建产物
clean:
	rm -rf $(BIN_DIR)
	rm -rf $(BACKEND_DIR)/bin
	rm -rf $(UI_DIR)/apps/web-antd/dist
