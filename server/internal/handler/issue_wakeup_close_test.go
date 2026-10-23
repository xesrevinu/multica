package handler

import (

)

// Closing an issue ends its wakeups in the status write's own transaction, not
// through a database trigger, on every writer that can close an issue.
