package store

const sweep = `DELETE FROM transitions WHERE observed_at < ?`
