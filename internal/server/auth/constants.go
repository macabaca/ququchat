package auth

import "time"

const (
    DefaultAccessTTL        = 15 * time.Minute
    DefaultRefreshTTL       = 30 * 24 * time.Hour
    DefaultRefreshTokenBytes = 32
)