# Changelog

## Unreleased

- `AutoBind` nella radice Build/BuildWithCleanup, ereditarietà nei moduli e
  override locale con `AutoBind(true/false)`, inclusi gli scope annidati.

- Help CLI strutturato con descrizioni, opzioni, default, esempi e colori su
  terminale; help per comando con exit code 0 e diagnostica di sintassi concisa.

- Autobinding attivo nei moduli di discovery degli skeleton hello-world e API;
  i binding manuali sono necessari soltanto per selezionare fra più implementazioni.

- Mapper degli errori iniettato negli handler stdlib ed Echo, con provider
  annotato e binding sostituibile nei template API.

- Provider framework di fallback per logger e lifecycle HTTP, sostituibili da
  input, provider e binding applicativi senza nascondere errori o ambiguità.
- Template API senza factory del logger e wrapper lifecycle nell'applicazione;
  il main riporta gli errori di bootstrap su stderr senza costruire un altro logger.

- Interfaccia `logging.Logger` sostituibile tramite DI e implementazione standard
  basata su `log/slog`, con livelli, contesto e campi strutturati.
- Logger iniettato negli skeleton API per readiness, errori HTTP e lifecycle.

- Template con discovery `../...` e provider annotati `//pfw:coconut`, senza
  liste esplicite di costruttori.
- Log di readiness dell'API con indirizzo e ambiente, dopo il bind del listener,
  per router stdlib ed Echo; test di avvio e porta occupata inclusi negli skeleton.

## v0.1.0 — prima release, in preparazione

Prima versione pubblica di Palma Framework. L'API è iniziale: le versioni `0.x`
possono introdurre cambiamenti incompatibili, documentati nelle note di rilascio.
Richiede Go 1.26 o successivo.

- Dependency injection generata a build time, costruttori Go normali e binding
  espliciti o automatici per modulo.
- Discovery dei provider annotati `//pfw:coconut`, esclusioni e override.
- Cleanup delle risorse, propagazione degli errori e lifecycle applicativo con
  shutdown ordinato e integrazione `net/http`.
- Configurazione tipizzata, validazione e caricamento dei file dotenv.
- Ambienti con nomi scelti dall'applicazione e dichiarati esplicitamente;
  generazione di un solo grafo per ambiente.
- CLI `generate`, `inspect`, `run`, `new`, `templates` e `version`.
- Skeleton `hello-world` e API esagonale con cartelle `infrastructure`, router
  stdlib o Echo v5, storage memory e test HTTP.
- Licenza MIT, workflow CI e preparazione delle release con binari e checksum.

Gin, Chi e altri trasporti non sono inclusi nei template di questa versione.
