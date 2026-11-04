# Limitations

Behaviour that may surprise a user who keeps notes on a server or works on several devices. For
users; the limits of the protection are in [Security](security.md#what-is-not-protected), those of
search by meaning in [Search](search.md#semantic-search).

- Backup and Restore replace the whole set of notes on the other side, as in v1. Before deleting
  anything they ask, with the number of notes to delete.
- The sync phrase cannot be changed while the server holds notes.
- Logging out or shutting down closes Smartnote without the close question, as SIGTERM does:
  unsaved server backups are not made, the local notes are kept. Back up before logging out.
