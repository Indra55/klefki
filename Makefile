.PHONY: linux-demo linux-build desktop-frontend

linux-demo: linux-build
	./desktop/build/bin/klefki-desktop

linux-build: desktop-frontend
	pkg-config --print-errors --exists gtk+-3.0 webkit2gtk-4.1
	go -C desktop build -tags desktop,production,webkit2_41,onnx -o build/bin/klefki-desktop .

desktop-frontend: desktop/frontend/node_modules/.package-lock.json
	npm --prefix desktop/frontend run build

desktop/frontend/node_modules/.package-lock.json: desktop/frontend/package.json desktop/frontend/package-lock.json
	npm --prefix desktop/frontend ci --ignore-scripts --no-audit --no-fund
