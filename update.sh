#! /bin/bash
echo "Updating..."
cp /root/duckex/duckex-server /root/duckex/duckex-server.bak

PID=$(cat /root/duckex/duckex-server.pid)

echo "Killing process $PID..."
kill $PID

echo "Waiting for process to die..."

while kill -0 $PID; do
  sleep 1
done

echo "Process killed."

echo "Copy new server..."
cp /root/duckex-server /root/duckex/duckex-server
chmod +x /root/duckex/duckex-server

echo "Starting new server..."
nohup /root/duckex/duckex-server &

echo "New server started."
