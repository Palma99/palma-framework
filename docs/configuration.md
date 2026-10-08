# Configurazione da environment e file dotenv

`github.com/palma99/palma-framework/config`, importato come `pfwconfig`, carica
variabili di ambiente in struct Go, applica default espliciti e valida il risultato
prima di restituirlo. Una configurazione invalida restituisce il valore zero;
non viene restituita una configurazione parzialmente popolata.

```go
type Database struct {
    Host string `env:"HOST" envDefault:"localhost"`
    Port uint16 `env:"PORT" envDefault:"5432"`
    Password string `env:"PASSWORD" envRequired:"true"`
}

type Config struct {
    Address string `env:"HTTP_ADDRESS" envDefault:":8080"`
    Timeout time.Duration `env:"TIMEOUT" envDefault:"5s"`
    Origins []string `env:"ORIGINS"`
    Database Database `envPrefix:"DB_"`
}

cfg, err := pfwconfig.Load[Config](pfwconfig.Options{Prefix: "APP_"})
```

Le chiavi risultanti includono `APP_HTTP_ADDRESS`, `APP_TIMEOUT`, `APP_ORIGINS`
e `APP_DB_HOST`, `APP_DB_PORT`, `APP_DB_PASSWORD`. La struttura delle cartelle e
i nomi dei tipi non hanno significato speciale.

## Regole

- I campi scalari vengono caricati soltanto se hanno un tag `env`. Gli altri
  restano al valore zero. `env:"-"` esclude un campo o un intero sotto-albero.
- Le struct annidate vengono attraversate; `envPrefix` aggiunge un prefisso.
  Ogni foglia ha un proprio tag `env`. Le struct ricorsive sono rifiutate.
- `envDefault` si applica soltanto quando la chiave è assente. Una chiave presente
  con valore vuoto non viene sostituita dal default.
- `envRequired:"true"` richiede una chiave presente e non vuota. Non si combina
  con un default, per evitare un obbligo che verrebbe sempre soddisfatto implicitamente.
- Le stringhe mantengono spazi e contenuto originali; gli spazi non equivalgono
  al valore vuoto. La validazione dell'applicazione può imporre vincoli ulteriori.
- `Load[T]` richiede una struct. Supporta stringhe, bool, interi con controllo
  di overflow, numeri float finiti, `time.Duration` e tipi con `encoding.TextUnmarshaler`.
- Le liste usano la virgola, oppure `envSeparator`; ogni elemento viene ripulito
  dagli spazi esterni. Un valore vuoto rappresenta una lista vuota. Sono liste
  delimitate, non CSV con quoting; liste annidate e di pointer non sono supportate.
- Pointer scalari assenti restano nil. Pointer a struct annidate vengono allocati
  se almeno una foglia è presente o ha un default.
- Chiavi duplicate, tag incoerenti, tipi non supportati e tag su campi privati
  producono errori di schema. Variabili estranee alla struct vengono ignorate.

Il loader usa `os.LookupEnv` di default e non modifica le variabili del processo.
Le struct possono anche essere costruite direttamente dal chiamante.

## File `.env`

```go
cfg, err := pfwconfig.Load[Config](pfwconfig.Options{
    Prefix: "APP_",
    EnvFiles: []string{".env", ".env.local"},
    IgnoreMissingEnvFiles: true,
})
```

La precedenza è: lookup/environment del processo, file indicati, default nei tag.
I file vengono letti in ordine e gli ultimi prevalgono sui precedenti. Anche una
variabile presente ma vuota nell'environment prevale sui file e sui default.
La lettura costruisce una mappa locale e non chiama `os.Setenv`.

I percorsi relativi partono dalla directory corrente del processo; non vengono
cercati file nelle directory superiori. Senza `EnvFiles` non si legge alcun file.
Di default un file richiesto mancante è un errore; `IgnoreMissingEnvFiles` ignora
soltanto i file assenti, mai errori di lettura o sintassi. La sintassi è gestita
da [godotenv](https://github.com/joho/godotenv), con commenti, `export` e valori
quotati. Le eventuali espansioni di variabili seguono il parser del singolo file;
non vengono ricalcolate dopo la sovrapposizione delle sorgenti.
Gli errori di parsing indicano il file senza riportarne le righe o i valori.

## Sottostruct, per esempio database

Le variabili rimangono chiavi flat nel file, ma la configurazione Go può essere
organizzata in sottostruct. `envPrefix` compone le chiavi senza appiattire il tipo:

```go
type DatabaseConfig struct {
    Host string `env:"HOST"`
    Port uint16 `env:"PORT" envDefault:"5432"`
}

type Config struct {
    DB DatabaseConfig `envPrefix:"DB_"`
}
```

```dotenv
APP_DB_HOST=localhost
APP_DB_PORT=5432
```

Con `Prefix: "APP_"`, i valori diventano `cfg.DB.Host` e `cfg.DB.Port`.
Si possono annidare ulteriori sottostruct e concatenare i prefissi. Il supporto
funziona allo stesso modo per environment, file e lookup di test.

## Errori e validazione applicativa

Gli errori di parsing sono aggregati in `*pfwconfig.Error`. Ogni `FieldError`
identifica percorso del campo, chiave e motivo; valori e messaggi originali
dei parser o `TextUnmarshaler` non vengono inclusi. Si possono ispezionare con
`errors.As`, anche attraverso `Error.Unwrap()`.

Se la struct o il suo pointer implementa `Validate() error`, il loader invoca
la validazione dopo il parsing riuscito:

```go
func (c Config) Validate() error {
    if c.Timeout <= 0 {
        return pfwconfig.Invalid("Timeout", "must be positive")
    }
    return nil
}
```

La validazione può aggregare vincoli con `errors.Join`. Questi messaggi sono
scelti dall'applicazione: devono descrivere il vincolo senza contenere segreti.
Per una struct costruita manualmente, è il chiamante a invocare `Validate`.

## Test e DI

Un lookup isolato evita di modificare l'environment globale:

```go
values := map[string]string{"APP_TIMEOUT": "2s"}
cfg, err := pfwconfig.Load[Config](pfwconfig.Options{
    Prefix: "APP_",
    Lookup: func(key string) (string, bool) {
        value, exists := values[key]
        return value, exists
    },
})
```

L'applicazione carica la configurazione prima di chiamare l'initializer generato,
poi la passa come input tipizzato. Il loader usa reflection per decodificare
la configurazione; la DI rimane generata a build time, senza reflection.

## Esempio HTTP

`examples/httpapi` usa il prefisso `HTTPAPI_`:

| Variabile | Default |
| --- | --- |
| `HTTPAPI_HTTP_ADDRESS` | `:8080` |
| `HTTPAPI_SEED_USER_NAME` | `Ada` |
| `HTTPAPI_HTTP_READ_HEADER_TIMEOUT` | `5s` |
| `HTTPAPI_HTTP_SHUTDOWN_TIMEOUT` | `5s` |

Indirizzo e timeout vengono validati prima del wiring e dell'apertura della porta.
Il nome iniziale viene verificato usando le regole del dominio.

```sh
cp examples/httpapi/.env.example .env
HTTPAPI_HTTP_ADDRESS=:9090 HTTPAPI_HTTP_SHUTDOWN_TIMEOUT=10s go run ./examples/httpapi/cmd/api
```

L'esempio legge `.env` dalla directory corrente, se presente, e usa sottostruct
`Config.HTTP` e `Config.Seed`. I nomi dei timeout ora includono il prefisso
`HTTP_` del gruppo. Non usa un database reale; la sottostruct DB sopra mostra
come configurarlo in un'applicazione che lo utilizza.
