# websocket

`github.com/gorilla/websocket`を基盤とする、Go向けのWebSocket transportモジュールです。

接続単位のread/write loop、ping/pong、deadline、送信キューを`Session`が管理し、メッセージの解析や認証などのアプリケーションロジックは`SessionHandler`へ委譲します。

## 必要環境

- Go 1.25.4以降

## インストール

```bash
go get github.com/k4k3ru-hub/websocket/go
```

```go
import websocket "github.com/k4k3ru-hub/websocket/go"
```

## 主な構成

- `Session`: 1つのWebSocket接続と、そのread/write lifecycleを管理します。
- `Server`: HTTP requestをWebSocketへUpgradeし、Server側Sessionを開始します。
- `Client`: lazy connect、送信、購読状態の保存、再購読を管理します。
- `Registry`: Sessionと任意のsubscription keyの対応をスレッドセーフに管理します。
- `SessionHandler`: 受信メッセージと切断イベントをアプリケーションへ通知します。

## SessionHandler

ServerとClientは共通の`SessionHandler`を使用します。

```go
type Handler struct{}

func (h *Handler) HandleMessage(sess websocket.SessionContext, message []byte) {
	// Decode, validate, authenticate, or route the application message.
	if err := sess.Send(message); err != nil {
		sess.Close()
	}
}

func (h *Handler) HandleClose(sess websocket.SessionContext) {
	// Release application resources associated with sess.ID().
}
```

`HandleMessage`はSessionのread loopから呼び出されます。時間のかかる処理を同期的に実行すると、そのSessionの後続メッセージ読み取りが停止するため、必要に応じてアプリケーション側で処理を分離してください。

## Server

```go
package main

import (
	"context"
	"log"
	"net/http"

	websocket "github.com/k4k3ru-hub/websocket/go"
)

func main() {
	option := websocket.DefaultServerOption()

	server, err := websocket.NewServer(
		context.Background(),
		&Handler{},
		option,
	)
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		if !websocket.IsWebSocketUpgrade(r) {
			http.Error(w, "websocket upgrade required", http.StatusBadRequest)
			return
		}

		if err := server.StartSession(w, r); err != nil {
			log.Printf("failed to start websocket session: %v", err)
		}
	})

	log.Fatal(http.ListenAndServe(":8080", nil))
}
```

### 初回メッセージのタイムアウト

`DefaultServerOption()`は、接続確立後5秒以内に最初の非空Text/Binaryメッセージを受信しなかったSessionを自動的に切断します。ping、pong、空メッセージではタイマーは解除されません。

```go
option := websocket.DefaultServerOption()
option.SessionOption.InitialMessageReadWaitTimeout = 10 * time.Second
```

無効化する場合は`0`を設定します。

```go
option.SessionOption.InitialMessageReadWaitTimeout = 0
```

Client向けの`DefaultSessionOption()`では、このタイムアウトはデフォルトで無効です。

### Originの検証

`DefaultServerOption()`の`CheckOrigin`はすべてのOriginを許可します。公開Serverでは、信頼できるOriginだけを許可する関数へ必ず置き換えてください。

```go
option.CheckOrigin = func(r *http.Request) bool {
	return r.Header.Get("Origin") == "https://example.com"
}
```

## Client

`Client`は`Connect()`を明示的に呼ばなくても、最初の送信または購読時に接続します。

```go
client, err := websocket.NewClient(
	context.Background(),
	"wss://example.com/ws",
	&Handler{},
	websocket.DefaultClientOption(),
)
if err != nil {
	return err
}
defer client.Close()

if err := client.SendRaw(ctx, []byte(`{"type":"ping"}`)); err != nil {
	return err
}
```

### 購読と再購読

`Subscribe`は、再購読に使用するraw payloadをkey単位で保存します。同じkeyへの重複した購読はno-opです。

```go
err := client.Subscribe(
	ctx,
	"ticker:BTC",
	[]byte(`{"method":"subscribe","channel":"ticker","symbol":"BTC"}`),
)
if err != nil {
	return err
}
```

切断後に保存済みpayloadを再送する場合は`ResubscribeAll`を使用します。

```go
if err := client.ResubscribeAll(ctx); err != nil {
	return err
}
```

購読解除payloadの送信に成功すると、保存済みの購読も削除されます。

```go
err := client.Unsubscribe(
	ctx,
	"ticker:BTC",
	[]byte(`{"method":"unsubscribe","channel":"ticker","symbol":"BTC"}`),
)
```

## Registry

`Registry`はSessionの登録と、subscription keyへのbindingを管理します。

```go
registry := websocket.NewRegistry()

if err := registry.Register(sess); err != nil {
	return err
}

result, err := registry.Bind("ticker:BTC", sess.ID())
if err != nil {
	return err
}

if result.First {
	// This is the first session bound to ticker:BTC.
}
```

`Unbind`はkeyとの関連だけを削除します。すべてのbindingがなくなっても、登録済みSessionはRegistryに残ります。Session自体を削除するには`Unregister`または`UnregisterByID`を使用してください。

Session終了時のRegistry cleanupはアプリケーションの責務です。

```go
func (h *Handler) HandleClose(sess websocket.SessionContext) {
	h.registry.Unregister(sess)
}
```

## Sessionの既定値

| 設定 | 既定値 | 用途 |
|---|---:|---|
| `WriteWait` | 10秒 | 1回の書き込み期限 |
| `PongWait` | 70秒 | pong受信後の読み取り期限 |
| `PingPeriod` | 25秒 | ping送信間隔 |
| `InitialReadWait` | 70秒 | Session開始直後の読み取り期限 |
| `InitialMessageReadWaitTimeout` | Client: 無効 / Server: 5秒 | 最初の非空Text/Binaryメッセージを待つ期限 |
| `MaxMessageSize` | 256 KiB | 受信メッセージ上限 |
| `SendQueueSize` | 256 | 非同期送信キュー容量 |

## 並行処理とライフサイクル

- `Session.Send`と`Session.SendJSON`はメッセージをコピーして送信キューへ追加します。
- 送信キューが満杯の場合は`ErrSendQueueFull`を返します。
- 閉じたSessionへの送信は`ErrSessionClosed`を返します。
- WebSocketへの書き込みはSessionのwrite loopに集約されています。
- `Client.Close`、`Session.Close`、`Registry`の操作は並行呼び出しを考慮しています。
- `Client.Close`後にClientを再接続することはできません。

## 検証

```bash
cd go
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

## License

[LICENSE](LICENSE)を参照してください。
