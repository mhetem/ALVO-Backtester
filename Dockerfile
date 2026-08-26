FROM node:22-alpine AS frontend
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/frontend/dist ./frontend/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /alvo .

FROM golang:1.25-alpine AS dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
EXPOSE 8080
CMD ["go", "run", "."]

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
COPY --from=build /alvo /alvo
EXPOSE 8080
USER nonroot:nonroot
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD ["/alvo", "healthcheck"]
ENTRYPOINT ["/alvo"]
