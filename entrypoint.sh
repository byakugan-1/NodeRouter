#!/bin/sh
# Entry point script for NodeRouter with Tor support

# Read Tor enabled status from config.yaml
TOR_ENABLED=$(grep -A1 "^tor:" /app/config.yaml | grep "enabled:" | awk '{print $2}')

if [ "$TOR_ENABLED" = "true" ]; then
    echo "[Tor] Tor enabled, starting hidden service..."
    
    # Ensure Tor data directory exists
    mkdir -p /var/lib/tor/noderouter
    chmod 700 /var/lib/tor/noderouter
    
    # Generate torrc - HiddenServicePort maps port 80 to local 5000
    cat > /var/lib/tor/noderouter/torrc << 'EOF'
SocksPort 0.0.0.0:9050
HiddenServiceDir /var/lib/tor/noderouter
HiddenServicePort 80 127.0.0.1:5000
HiddenServiceVersion 3
EOF
    
    # Start Tor in background
    tor -f /var/lib/tor/noderouter/torrc &
    TOR_PID=$!
    echo "[Tor] Tor started with PID $TOR_PID"
    
    # Wait for hostname file
    for i in $(seq 1 30); do
        if [ -f /var/lib/tor/noderouter/hostname ]; then
            HOSTNAME=$(cat /var/lib/tor/noderouter/hostname)
            echo "[Tor] Hidden service ready: $HOSTNAME"
            break
        fi
        sleep 1
    done
else
    echo "[Tor] Tor disabled in config"
fi

# Start NodeRouter
exec /app/noderouter "$@"
