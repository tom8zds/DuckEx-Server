 $env:GOOS='windows'; go build -o duckex-server.exe .\cmd\api\main.go
 $env:GOOS='linux'; go build -o duckex-server .\cmd\api\main.go