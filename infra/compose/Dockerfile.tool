# Local mocks (SMS gateway, payment rail). Never deployed anywhere real.
FROM golang:1.25-alpine AS build
ARG TOOL
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/tool ./tools/mocks/${TOOL}

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/tool /mock
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/mock"]
