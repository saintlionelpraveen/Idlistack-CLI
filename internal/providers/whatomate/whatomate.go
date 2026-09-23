package whatomate

import (
	"os"
	"path/filepath"

	"github.com/idlistack/cli/internal/buildplan"
	"github.com/idlistack/cli/internal/provider"
)

type WhatomateProvider struct{}

func (p *WhatomateProvider) Name() string {
	return "whatomate"
}

func (p *WhatomateProvider) Detect(ctx *provider.DetectContext) (bool, error) {
	// Dynamically detect whether we are in the root directory (w1/) or inside w1/
	return ctx.App.HasDir("w1/cmd/whatomate") || ctx.App.HasDir("cmd/whatomate"), nil
}

func (p *WhatomateProvider) Initialize(ctx *provider.DetectContext) error {
	return nil
}

func (p *WhatomateProvider) Plan(ctx *provider.DetectContext) (*buildplan.Plan, error) {
	plan := buildplan.NewDefaultPlan()
	plan.Provider = "whatomate"
	plan.DetectedFramework = "whatomate"
	plan.Runtime = "ubuntu"
	
	// Override config with dynamic environment variables
	plan.Env = map[string]string{
		"WHATOMATE_SERVER__ALLOWED_ORIGINS": "{{APP_URL}}",
		"WHATOMATE_COOKIE__SECURE":          "false",
		"WHATOMATE_APP__ENVIRONMENT":        "development", // Must be development so setDefaults doesn't force secure=true on HTTP!
		"DB_HOST":                           "{{DB_HOST}}",
		"DB_PORT":                           "{{DB_PORT}}",
	}

	// Determine source directory path based on where we are
	srcDir := "."
	if ctx.App.HasDir("w1/cmd/whatomate") {
		srcDir = "w1"
	}

	// Generate a comprehensive, production-ready zero-config Dockerfile 
	// that runs both Go Dashboard and Node Middleware with Nginx routing
	template := `FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend-builder
WORKDIR /app/frontend
COPY ` + srcDir + `/frontend/package.json ` + srcDir + `/frontend/package-lock.json ./
RUN npm cache clean --force && npm ci
COPY ` + srcDir + `/frontend/ .
RUN rm -rf node_modules/.vite && npm run build

FROM --platform=$BUILDPLATFORM node:20-alpine AS middleware-builder
WORKDIR /app/middleware
RUN apk add --no-cache python3 make g++ git sqlite-dev
COPY ` + srcDir + `/middleware/package*.json ./
RUN npm install --build-from-source sqlite3
RUN npm install
COPY ` + srcDir + `/middleware/ .

FROM --platform=$BUILDPLATFORM golang:1.25.3-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /app
RUN apk add --no-cache git ca-certificates
COPY ` + srcDir + `/go.mod ` + srcDir + `/go.sum ./
RUN go mod download
COPY ` + srcDir + `/ .
COPY --from=frontend-builder /app/frontend/dist/ ./internal/frontend/dist/
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -a -installsuffix cgo -o whatomate ./cmd/whatomate

FROM --platform=$BUILDPLATFORM debian:bookworm-slim AS piper-dl
ARG TARGETARCH
RUN apt-get update && apt-get install -y --no-install-recommends wget ca-certificates && rm -rf /var/lib/apt/lists/*
RUN case "${TARGETARCH:-amd64}" in amd64) PIPER_ARCH=x86_64 ;; arm64) PIPER_ARCH=aarch64 ;; *) echo "unsupported TARGETARCH" >&2; exit 1 ;; esac \
    && wget -q "https://github.com/rhasspy/piper/releases/download/2023.11.14-2/piper_linux_${PIPER_ARCH}.tar.gz" -O /tmp/piper.tar.gz \
    && tar xf /tmp/piper.tar.gz -C /tmp && rm /tmp/piper.tar.gz
RUN mkdir -p /tmp/piper-models \
    && wget -q https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_US/lessac/medium/en_US-lessac-medium.onnx -O /tmp/piper-models/en_US-lessac-medium.onnx \
    && wget -q https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_US/lessac/medium/en_US-lessac-medium.onnx.json -O /tmp/piper-models/en_US-lessac-medium.onnx.json

FROM debian:bookworm-slim
WORKDIR /app
ENV NODE_ENV=production

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates tzdata espeak-ng opus-tools ffmpeg curl nginx && \
    curl -fsSL https://deb.nodesource.com/setup_20.x | bash - && \
    apt-get install -y --no-install-recommends nodejs && \
    rm -rf /var/lib/apt/lists/*

# Install Piper TTS
COPY --from=piper-dl /tmp/piper/piper /usr/local/bin/piper
COPY --from=piper-dl /tmp/piper/lib*.so* /usr/local/lib/
COPY --from=piper-dl /tmp/piper/espeak-ng-data /usr/share/espeak-ng-data
RUN ldconfig
COPY --from=piper-dl /tmp/piper-models /opt/piper/models

# Whatomate Dashboard
COPY --from=builder /app/whatomate .
COPY --from=builder /app/docker/config.toml ./config.toml
RUN mkdir -p /app/uploads /app/audio

# Whatomate Middleware
COPY --from=middleware-builder /app/middleware ./middleware

# Nginx config for routing
RUN echo "server {" > /etc/nginx/sites-available/default && \
    echo "    listen 80;" >> /etc/nginx/sites-available/default && \
    echo "    location / {" >> /etc/nginx/sites-available/default && \
    echo "        proxy_pass http://127.0.0.1:3000;" >> /etc/nginx/sites-available/default && \
    echo "        proxy_http_version 1.1;" >> /etc/nginx/sites-available/default && \
    echo "        proxy_set_header Upgrade \$http_upgrade;" >> /etc/nginx/sites-available/default && \
    echo "        proxy_set_header Connection \"upgrade\";" >> /etc/nginx/sites-available/default && \
    echo "        proxy_set_header Host \$host;" >> /etc/nginx/sites-available/default && \
    echo "        proxy_set_header X-Real-IP \$remote_addr;" >> /etc/nginx/sites-available/default && \
    echo "        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;" >> /etc/nginx/sites-available/default && \
    echo "        proxy_set_header X-Forwarded-Proto \$scheme;" >> /etc/nginx/sites-available/default && \
    echo "    }" >> /etc/nginx/sites-available/default && \
    echo "    location ~ ^/(webhook|submissions|api/webhook|api/middleware) {" >> /etc/nginx/sites-available/default && \
    echo "        proxy_pass http://127.0.0.1:8080;" >> /etc/nginx/sites-available/default && \
    echo "        proxy_http_version 1.1;" >> /etc/nginx/sites-available/default && \
    echo "        proxy_set_header Host \$host;" >> /etc/nginx/sites-available/default && \
    echo "        proxy_set_header X-Real-IP \$remote_addr;" >> /etc/nginx/sites-available/default && \
    echo "        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;" >> /etc/nginx/sites-available/default && \
    echo "        proxy_set_header X-Forwarded-Proto \$scheme;" >> /etc/nginx/sites-available/default && \
    echo "    }" >> /etc/nginx/sites-available/default && \
    echo "}" >> /etc/nginx/sites-available/default

# Start script
RUN echo "#!/bin/bash" > /app/start.sh && \
    echo "if [ ! -z \"\$DB_HOST\" ]; then sed -i 's/^host = .*/host = \"'\$DB_HOST'\"/' /app/config.toml; fi" >> /app/start.sh && \
    echo "if [ ! -z \"\$DB_PORT\" ]; then sed -i 's/^port = .*/port = '\$DB_PORT'/' /app/config.toml; fi" >> /app/start.sh && \
    echo "nginx" >> /app/start.sh && \
    echo "echo 'Starting Node Middleware...'" >> /app/start.sh && \
    echo "cd /app/middleware && npm start &" >> /app/start.sh && \
    echo "echo 'Starting Go Dashboard...'" >> /app/start.sh && \
    echo "cd /app && ./whatomate server -config config.toml -migrate" >> /app/start.sh && \
    chmod +x /app/start.sh

EXPOSE 80
CMD ["/app/start.sh"]
`

	// Write the generated Dockerfile
	outDir := filepath.Join(ctx.App.Source, ".idlistack")
	os.MkdirAll(outDir, 0755)
	outPath := filepath.Join(outDir, "Dockerfile.whatomate")
	os.WriteFile(outPath, []byte(template), 0644)
	
	plan.DockerfilePath = filepath.Join(".idlistack", "Dockerfile.whatomate")

	return plan, nil
}
