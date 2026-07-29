// Covers the Steam authority lookup followed by a long AI description translation.
export const AI_OPERATION_TIMEOUT_MS = 120_000;

// Save archives can be large and the import endpoint performs bounded upload,
// validation, and extraction before responding. Axios uses 0 for no client-side
// timeout; server-side upload and archive limits still apply.
export const SAVE_ARCHIVE_IMPORT_TIMEOUT_MS = 0;

// Save indexing is bounded by the backend sidecar timeout (120 seconds by
// default), with additional time for process startup and response handling.
export const SAVE_INDEX_OPERATION_TIMEOUT_MS = 180_000;

// Runtime RCON inspection can wait up to 30 seconds for each compatibility form
// of /getrconcmds, plus response and network handling time.
export const RCON_COMMAND_INSPECTION_TIMEOUT_MS = 70_000;
