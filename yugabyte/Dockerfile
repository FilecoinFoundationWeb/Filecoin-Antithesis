# Use the official YugabyteDB image from Docker Hub as the base image
FROM yugabytedb/yugabyte:latest

# Set environment variables (optional, adjust as needed)
# ENV YB_NUM_SHARDS=3

# Create the necessary directories
RUN mkdir -p /mnt/disk/data

# Expose required ports (YugabyteDB default ports)
EXPOSE 7000 7100 9000 5433
