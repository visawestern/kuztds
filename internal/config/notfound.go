package config

// MaxNotFoundBytes bounds the custom 404 page (G-132). A page is a small
// static document; a larger file is a mistake or a file host.
const MaxNotFoundBytes = 64 << 10
