.PHONY: linux-demo linux-build linux-install linux-update linux-rollback desktop-frontend bench-local bench-hosted

bench-local:
	npm --prefix web run benchmark:local

bench-hosted:
	@test -n "$(BENCH_SERVER)" || { echo 'Use make bench-hosted BENCH_SERVER=https://YOUR-RELAY'; exit 1; }
	npm --prefix web run benchmark -- --server "$(BENCH_SERVER)"

linux-demo: linux-build
	./desktop/build/bin/klefki-desktop

linux-install: linux-build
	python3 scripts/install-linux.py

linux-update: linux-install

linux-rollback:
	python3 scripts/install-linux.py --rollback

linux-build: desktop-frontend
	pkg-config --print-errors --exists gtk+-3.0 webkit2gtk-4.1
	go -C desktop build -tags desktop,production,webkit2_41,onnx -o build/bin/klefki-desktop .

desktop-frontend: desktop/frontend/node_modules/.package-lock.json
	npm --prefix desktop/frontend run build

desktop/frontend/node_modules/.package-lock.json: desktop/frontend/package.json desktop/frontend/package-lock.json
	npm --prefix desktop/frontend ci --ignore-scripts --no-audit --no-fund
