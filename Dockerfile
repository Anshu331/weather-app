FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/weatherapp ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/weatherapp /weatherapp
ENV WEATHER_ADDR=:8080 WEATHER_DATA_DIR=/data
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/weatherapp"]
