# Changelog

## Unreleased

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
