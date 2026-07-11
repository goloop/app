[![Go Reference](https://img.shields.io/badge/godoc-reference-blue)](https://pkg.go.dev/github.com/goloop/app) [![License](https://img.shields.io/badge/license-MIT-brightgreen)](https://github.com/goloop/app/blob/master/LICENSE) [![Stay with Ukraine](https://img.shields.io/static/v1?label=Stay%20with&message=Ukraine%20♥&color=ffD700&labelColor=0057B8&style=flat)](https://u24.gov.ua/)

# app

`app` - невелике ядро життєвого циклу й композиції для Go-сервісів. Воно запускає
набір явно зареєстрованих компонентів із контрольованим життєвим циклом:
впорядкований старт, очікування сигналу, і graceful shutdown у зворотному
порядку з обмеженим таймаутом.

Це свідомо **не** фреймворк: без прихованого global state, без DI-контейнера, без
роутингу й парсингу конфігу. Обв'язка лишається явною й видимою у вашому `main`.
Нуль залежностей, лише стандартна бібліотека.

## Встановлення

```bash
go get github.com/goloop/app
```

## Швидкий старт

```go
func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	mux := http.NewServeMux()
	// ... реєстрація хендлерів ...

	a := app.New("api",
		app.WithLogger(slog.Default()),
		app.WithShutdownTimeout(10*time.Second),
	)

	a.Use(app.HTTPServer("http", &http.Server{Addr: ":8080", Handler: mux}))
	a.OnStop(func(context.Context) error {
		pool.Close()
		return nil
	})

	return a.Run(context.Background())
}
```

`Run` стартує компоненти в порядку реєстрації, чекає `SIGINT`/`SIGTERM` (або
скасування батьківського контексту, або фатальну помилку компонента), тоді
зупиняє компоненти у зворотному порядку й виконує stop-хуки. Повертає nil при
чистому shutdown за сигналом. Другий сигнал під час shutdown форсує негайний
вихід.

## Компоненти

Компонент має ім'я, неблокуючий `Start` і `Stop`:

```go
type Component interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}
```

`Start` запускає роботу (зазвичай горутину) і повертається, коли старт удався,
або помилку, якщо старт провалився синхронно. Про фонову помилку компонент
повідомляє через `app.Fatal(ctx, err)` - це запускає graceful shutdown.

Готові компоненти:

| Конструктор | Start | Stop |
|-------------|-------|------|
| `HTTPServer(name, *http.Server)` | bind + `Serve` у горутині | `srv.Shutdown` |
| `Worker(name, func(ctx) error)` | виконати блокуючу функцію | чекати її завершення |
| `Closer(name, func() error)` | нічого | викликати cleanup-функцію |

## Health без зв'язності

App не віддає HTTP і нічого не знає про «health». Він експонує `Status()` -
read-only знімок стану компонентів як звичайні дані:

```go
st := a.Status()
if !st.Healthy() { /* компонент впав */ }
```

Зовнішній health-реєстр (наприклад `goloop/observe`) перетворює цей знімок на
check і монтує власні хендлери - тож `app` лишається незв'язаним.

## Документація

- Англійський довідник: [DOC.md](DOC.md)
- Український довідник: [DOC.UK.md](DOC.UK.md)

## Ліцензія

MIT - див. [LICENSE](LICENSE).
