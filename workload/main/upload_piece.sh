#!/bin/bash

set -e  # Exit on error

# Configuration
PDP_URL=${PDP_URL:-"http://curio:80"}  # Can be overridden by environment variable
CONTRACT_PATH="/root/devgen/contracts/service-implementation.addr"
JWT_PATH="/root/devgen/contracts/jwt_token.txt"
TEST_FILE="test_piece.txt"

# Read the record keeper address from contract file
if [ ! -f "$CONTRACT_PATH" ]; then
    echo "Contract address file not found at $CONTRACT_PATH"
    exit 1
fi
RECORD_KEEPER=$(cat "$CONTRACT_PATH")
echo "Using record keeper address: $RECORD_KEEPER"

# Check for JWT token
if [ ! -f "$JWT_PATH" ]; then
    echo "JWT token file not found at $JWT_PATH"
    exit 1
fi

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Helper function to make requests
make_request() {
    local method=$1
    local endpoint=$2
    local data=$3
    local extra_headers=$4

    if [ -n "$data" ]; then
        curl -s -X "$method" "${PDP_URL}${endpoint}" \
            -H "Authorization: Bearer $(cat $JWT_PATH)" \
            -H "Content-Type: application/json" \
            $extra_headers \
            -d "$data"
    else
        curl -s -X "$method" "${PDP_URL}${endpoint}" \
            -H "Authorization: Bearer $(cat $JWT_PATH)" \
            $extra_headers
    fi
}

echo -e "${GREEN}Starting PDP Piece Upload Test${NC}"

# 1. Test connectivity
echo -e "\n${GREEN}1. Testing PDP connectivity...${NC}"
response=$(make_request "GET" "/pdp/ping")
if [ $? -eq 0 ]; then
    echo "✓ PDP service is reachable"
else
    echo -e "${RED}✗ Failed to reach PDP service${NC}"
    exit 1
fi

# 2. Create a test file with random data (1 MB)
echo -e "\n${GREEN}2. Creating test file (1 MB)...${NC}"
dd if=/dev/urandom of=$TEST_FILE bs=1M count=1 2>/dev/null
if [ $? -eq 0 ]; then
    echo "✓ Created test file: $TEST_FILE"
else
    echo -e "${RED}✗ Failed to create test file${NC}"
    exit 1
fi

# 3. Calculate file hash and size
echo -e "\n${GREEN}3. Calculating file details...${NC}"
FILE_HASH=$(sha256sum $TEST_FILE | cut -d' ' -f1)
FILE_SIZE=$(stat -f%z $TEST_FILE 2>/dev/null || stat -c%s $TEST_FILE)
echo "File hash: $FILE_HASH"
echo "File size: $FILE_SIZE bytes"

# 4. Initiate piece upload
echo -e "\n${GREEN}4. Initiating piece upload...${NC}"
UPLOAD_RESPONSE=$(make_request "POST" "/pdp/piece" "{
    \"check\": {
        \"name\": \"sha2-256\",
        \"hash\": \"$FILE_HASH\",
        \"size\": $FILE_SIZE
    }
}" "-i")  # Include headers in response

echo "Upload initiation response:"
echo "$UPLOAD_RESPONSE"

# Extract upload URL if status is 201
if echo "$UPLOAD_RESPONSE" | grep -q "HTTP/1.1 201"; then
    UPLOAD_URL=$(echo "$UPLOAD_RESPONSE" | grep -i "Location:" | cut -d' ' -f2 | tr -d '\r')
    echo "Upload URL: $UPLOAD_URL"

    # 5. Upload the actual file
    echo -e "\n${GREEN}5. Uploading file data...${NC}"
    UPLOAD_RESULT=$(curl -s -X PUT "${PDP_URL}${UPLOAD_URL}" \
        -H "Authorization: Bearer $(cat $JWT_PATH)" \
        -H "Content-Type: application/octet-stream" \
        --data-binary @"$TEST_FILE" \
        -w "%{http_code}")
    
    if [ "$UPLOAD_RESULT" = "204" ]; then
        echo "✓ File upload successful"
    else
        echo -e "${RED}✗ File upload failed with status: $UPLOAD_RESULT${NC}"
        exit 1
    fi
elif echo "$UPLOAD_RESPONSE" | grep -q "HTTP/1.1 200"; then
    echo "✓ Piece already exists"
    PIECE_CID=$(echo "$UPLOAD_RESPONSE" | grep -o '"pieceCID":"[^"]*"' | cut -d'"' -f4)
    echo "Piece CID: $PIECE_CID"
else
    echo -e "${RED}✗ Unexpected response during upload initiation${NC}"
    exit 1
fi

# 6. Create a proof set
echo -e "\n${GREEN}6. Creating proof set...${NC}"
PROOFSET_RESPONSE=$(make_request "POST" "/pdp/proof-sets" "{
    \"recordKeeper\": \"$RECORD_KEEPER\"
}" "-i")

echo "Proof set creation response:"
echo "$PROOFSET_RESPONSE"

# Extract creation status URL
if echo "$PROOFSET_RESPONSE" | grep -q "HTTP/1.1 201"; then
    STATUS_URL=$(echo "$PROOFSET_RESPONSE" | grep -i "Location:" | cut -d' ' -f2 | tr -d '\r')
    echo "Status URL: $STATUS_URL"

    # 7. Check proof set creation status
    echo -e "\n${GREEN}7. Checking proof set creation status...${NC}"
    max_attempts=10
    attempt=1
    while [ $attempt -le $max_attempts ]; do
        STATUS_RESPONSE=$(make_request "GET" "$STATUS_URL")
        echo "Status check attempt $attempt:"
        echo "$STATUS_RESPONSE"
        
        if echo "$STATUS_RESPONSE" | grep -q '"proofsetCreated":true'; then
            PROOFSET_ID=$(echo "$STATUS_RESPONSE" | grep -o '"proofSetId":[0-9]*' | cut -d':' -f2)
            echo "✓ Proof set created with ID: $PROOFSET_ID"
            break
        fi
        
        echo "Waiting for proof set creation..."
        sleep 5
        attempt=$((attempt + 1))
    done

    if [ $attempt -gt $max_attempts ]; then
        echo -e "${RED}✗ Proof set creation timed out${NC}"
        exit 1
    fi

    # 8. Get proof set details
    if [ -n "$PROOFSET_ID" ]; then
        echo -e "\n${GREEN}8. Getting proof set details...${NC}"
        DETAILS_RESPONSE=$(make_request "GET" "/pdp/proof-sets/$PROOFSET_ID")
        echo "Proof set details:"
        echo "$DETAILS_RESPONSE"

        # 9. Add roots to proof set (if we have the piece CID)
        if [ -n "$PIECE_CID" ]; then
            echo -e "\n${GREEN}9. Adding root to proof set...${NC}"
            ROOT_RESPONSE=$(make_request "POST" "/pdp/proof-sets/$PROOFSET_ID/roots" "[{
                \"rootCid\": \"$PIECE_CID\",
                \"subroots\": [{
                    \"subrootCid\": \"$PIECE_CID\"
                }]
            }]")
            echo "Root addition response:"
            echo "$ROOT_RESPONSE"
        fi
    fi
else
    echo -e "${RED}✗ Failed to create proof set${NC}"
    exit 1
fi

# Cleanup
echo -e "\n${GREEN}Cleaning up...${NC}"
rm -f $TEST_FILE

echo -e "\n${GREEN}PDP Piece Upload Test Complete${NC}" 