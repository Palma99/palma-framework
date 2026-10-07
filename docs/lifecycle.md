# Lifecycle applicativo

Il package `lifecycle` gestisce server e worker senza dipendere da HTTP.
Il codice di composizione decide quali componenti avviare, in quale ordine
e quale cleanup della DI affidare al lifecycle.

## Utilizzo

Con un initializer generato tramite `BuildWithCleanup`:

```go
server, cleanup, err := Initialize(cfg)
if err != nil {
    return err // Il wiring ha già eseguito il rollback.
}
app := lifecycle.New(lifecycle.Options{
    ShutdownTimeout: 5 * time.Second,
    Cleanup: cleanup,
}, httpserver.New(server))
return app.RunSignals()
```

Gli import dell'esempio sono `time`,
`github.com/palma99/palma-framework/lifecycle` e
`github.com/palma99/palma-framework/transport/httpserver`.
Se l'initializer non possiede risorse può restare nella forma `(T, error)` e
l'opzione `Cleanup` può essere omessa, come in `examples/httpapi`.
Non aggiungere anche un `defer cleanup()` quando il callback viene affidato
all'applicazione: il lifecycle ne gestisce la chiamata e l'errore.

`RunSignals` gestisce SIGINT e SIGTERM. `Run(ctx)` permette invece di fornire
cancellazione o deadline dall'esterno, utile nei test e nell'embedding.
Ogni istanza di `App` può essere eseguita una sola volta; invocazioni successive
o concorrenti restituiscono `ErrAlreadyRun` senza ripetere il cleanup.

## Componenti e ordine delle operazioni

```go
type Component interface {
    Start(context.Context) error
    Wait() error
    Stop(context.Context) error
}
```

- `Start` avvia il componente e ritorna quando è pronto. Deve rispettare il
  context, che viene cancellato anche se un componente già avviato termina.
- `Wait` attende la fine dell'esecuzione. Ritorna nil per una chiusura normale,
  oppure un errore per un guasto. La fine di un componente termina l'applicazione.
- `Stop` arresta il componente rispettando il context e permettendo a `Wait`
  di terminare. Deve poter ripulire anche un avvio parziale che ha restituito errore.

I componenti vengono avviati nell'ordine passato a `New` e arrestati nell'ordine
inverso. Se l'avvio fallisce, viene arrestato anche il componente che ha fallito,
oltre a quelli precedenti. I componenti successivi non vengono avviati.

Alla cancellazione, fine di un componente o errore, il lifecycle:

1. Cancella il context di esecuzione.
2. Arresta i componenti utilizzando un nuovo context con timeout condiviso.
3. Attende il termine dell'esecuzione entro quella stessa deadline.
4. Invoca il cleanup delle risorse DI.

Il timeout predefinito è 10 secondi. Un componente deve rispettare la deadline
di `Stop`: il lifecycle non può interrompere forzatamente codice Go arbitrario.
Il callback DI `func() error` non riceve context e non è limitato dal timeout
di shutdown; deve completare la chiusura delle risorse autonomamente.

Gli errori di avvio, esecuzione, arresto e cleanup vengono aggregati mantenendo
`errors.Is` e `errors.As`. La cancellazione ordinaria del chiamante termina
normalmente; gli errori restituiti dai componenti durante shutdown restano errori.

## Adapter HTTP iniziale

`transport/httpserver.New(*http.Server)` implementa `Component`.
L'adapter apre il listener prima di terminare `Start`: una porta occupata è
un errore di avvio. `Addr()` espone l'indirizzo effettivo, anche con porta zero.
`Stop` usa `Shutdown`; se la deadline scade, chiude forzatamente le connessioni.
La normale uscita di `Serve` con `http.ErrServerClosed` viene considerata riuscita.

Il router resta l'`http.Handler` scelto dall'applicazione. Il nucleo lifecycle
non importa `net/http` e non impone tipi di richieste, middleware o routing.
Altri server HTTP o gRPC possono essere collegati implementando lo stesso
contratto; adapter dedicati per quelle librerie non sono ancora inclusi.

`examples/httpapi` usa Echo v5 come handler del server HTTP. Le route usano
context, risposte JSON ed errori nativi di Echo, mentre il server viene avviato
e arrestato dal lifecycle. Riferimento: [Echo quickstart](https://echo.labstack.com/next/guide/quickstart/).
