#!/bin/sh
set -e

# If the first argument is "bot", run the catwatch_bot
if [ "$1" = "bot" ]; then
    shift
    exec /app/catwatch_bot "$@"
fi

# If the first argument is "catwatch", run the catwatch app
if [ "$1" = "catwatch" ]; then
    shift
    exec /app/catwatch "$@"
fi

# By default, try to run catwatch with all arguments
# This allows running like: docker run catwatch --debug
if [ "${1#-}" != "$1" ] || [ -z "$1" ]; then
    exec /app/catwatch "$@"
fi

# Otherwise, execute the command as provided (e.g., /bin/sh)
exec "$@"
