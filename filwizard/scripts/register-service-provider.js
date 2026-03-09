#!/usr/bin/env node

import { ethers } from 'ethers';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// Parse command line arguments
function parseArgs() {
  const args = process.argv.slice(2);
  const parsed = {};

  for (let i = 0; i < args.length; i++) {
    if (args[i].startsWith('--')) {
      const key = args[i].substring(2);
      const value = args[i + 1];
      parsed[key] = value;
      i++;
    }
  }

  return parsed;
}

const args = parseArgs();
const RPC_URL = args['rpc-url'] || process.env.RPC_URL;
const SP_REGISTRY_ADDRESS = args['sp-registry'] || process.env.SERVICE_PROVIDER_REGISTRY_PROXY_ADDRESS;
const FWSS_ADDRESS = args['warm-storage'] || process.env.FWSS_PROXY_ADDRESS;

// Environment variables
const DEPLOYER_PRIVATE_KEY = process.env.DEPLOYER_PRIVATE_KEY;
const SP_PRIVATE_KEY = process.env.SP_PRIVATE_KEY;
const SP_SERVICE_URL = process.env.SP_SERVICE_URL || 'http://curio:80';
const SP_NAME = process.env.SP_NAME || 'My Devnet Provider';
const SP_DESCRIPTION = process.env.SP_DESCRIPTION || 'Devnet provider for Warm Storage';

console.log('ℹ️  🚀 Starting service provider registration');
console.log(`ℹ️  RPC: ${RPC_URL}`);
console.log(`ℹ️  Warm Storage: ${FWSS_ADDRESS}`);
console.log(`ℹ️  SP Registry: ${SP_REGISTRY_ADDRESS}`);

// Load ABI
const WORKSPACE_PATH = '/opt/filwizard/workspace';
const registryAbiPath = path.join(WORKSPACE_PATH, 'filecoinwarmstorage', 'service_contracts', 'abi', 'ServiceProviderRegistry.abi.json');

let registryAbi;
try {
  registryAbi = JSON.parse(fs.readFileSync(registryAbiPath, 'utf8'));
} catch (error) {
  console.error('❌ Failed to load ServiceProviderRegistry ABI:', error.message);
  process.exit(1);
}

// Setup provider and wallets
const provider = new ethers.JsonRpcProvider(RPC_URL);
const deployerWallet = new ethers.Wallet(DEPLOYER_PRIVATE_KEY, provider);
const spWallet = new ethers.Wallet(SP_PRIVATE_KEY, provider);

console.log(`ℹ️  Deployer: ${deployerWallet.address}`);
console.log(`ℹ️  Service Provider: ${spWallet.address}`);
console.log('ℹ️  ');

// Create contract instance
const registry = new ethers.Contract(SP_REGISTRY_ADDRESS, registryAbi, spWallet);

// Registration parameters
const REGISTRATION_FEE = ethers.parseEther('5'); // 5 FIL

async function registerServiceProvider() {
  console.log('📋 Step 1: Service Provider Registration in Registry');

  // Check if already registered
  try {
    const providerId = await registry.addressToProviderId(spWallet.address);
    if (providerId > 0) {
      console.log(`✅ Service provider already registered with ID: ${providerId}`);
      return providerId;
    }
  } catch (error) {
    // Continue with registration
  }

  console.log(`ℹ️  Registering new provider: ${SP_NAME}`);
  console.log(`ℹ️  Note: Registration requires a 5 FIL fee`);

  // Check balance
  const balance = await provider.getBalance(spWallet.address);
  console.log(`ℹ️  SP Balance: ${ethers.formatEther(balance)} FIL`);
  console.log(`ℹ️  Registration Fee: ${ethers.formatEther(REGISTRATION_FEE)} FIL`);

  if (balance < REGISTRATION_FEE) {
    console.error(`❌ Insufficient balance. Need ${ethers.formatEther(REGISTRATION_FEE)} FIL, have ${ethers.formatEther(balance)} FIL`);
    process.exit(1);
  }

  // Prepare capabilities - MUST match ServiceProviderRegistry.sol REQUIRED_PDP_KEYS
  // Required capability keys (case-sensitive, must match exactly):
  // 1. serviceURL
  // 2. minPieceSizeInBytes
  // 3. maxPieceSizeInBytes
  // 4. storagePricePerTibPerDay
  // 5. minProvingPeriodInEpochs
  // 6. location
  // 7. paymentTokenAddress

  // Helper function to encode number as bytes (big-endian)
  const encodeNumber = (num) => {
    const bn = ethers.toBigInt(num);
    const hex = bn.toString(16);
    return '0x' + (hex.length % 2 === 0 ? hex : '0' + hex);
  };

  const capabilityKeys = [
    'serviceURL',
    'minPieceSizeInBytes',
    'maxPieceSizeInBytes',
    'storagePricePerTibPerDay',
    'minProvingPeriodInEpochs',
    'location',
    'paymentTokenAddress'
  ];

  // USDFC address for paymentTokenAddress — SP must declare which token it accepts
  const USDFC_ADDRESS = process.env.USDFC_ADDRESS;
  if (!USDFC_ADDRESS) {
    console.error('❌ USDFC_ADDRESS not set in environment');
    process.exit(1);
  }
  console.log(`ℹ️  USDFC Token: ${USDFC_ADDRESS}`);

  const capabilityValues = [
    ethers.hexlify(ethers.toUtf8Bytes(SP_SERVICE_URL)),                    // serviceURL (string)
    encodeNumber(1024),                                                      // minPieceSizeInBytes (1 KiB)
    encodeNumber(1024 * 1024 * 1024),                                       // maxPieceSizeInBytes (1 GiB)
    encodeNumber(1000000),                                                   // storagePricePerTibPerDay (1e6 attoUSDFC — matches SDK devnet)
    encodeNumber(30),                                                        // minProvingPeriodInEpochs (30 epochs ≈ 15 min — fast for testing)
    ethers.hexlify(ethers.toUtf8Bytes('us-east-1')),                        // location (string)
    ethers.zeroPadValue(USDFC_ADDRESS, 20)                                   // paymentTokenAddress (USDFC, not zero/FIL)
  ];

  // ProductType.PDP = 0
  const ProductType_PDP = 0;

  console.log('ℹ️  Submitting registration transaction...');

  try {
    const tx = await registry.registerProvider(
      spWallet.address,      // payee - same as service provider
      SP_NAME,               // name
      SP_DESCRIPTION,        // description
      ProductType_PDP,       // productType
      capabilityKeys,        // capabilityKeys
      capabilityValues,      // capabilityValues
      {
        value: REGISTRATION_FEE  // CRITICAL: Must send 5 FIL
      }
    );

    console.log(`ℹ️  Transaction submitted: ${tx.hash}`);
    console.log('ℹ️  Waiting for confirmation...');

    const receipt = await tx.wait();
    console.log(`✅ Transaction confirmed in block ${receipt.blockNumber}`);

    // Extract provider ID from events
    const event = receipt.logs
      .map(log => {
        try {
          return registry.interface.parseLog(log);
        } catch {
          return null;
        }
      })
      .find(e => e && e.name === 'ProviderRegistered');

    if (event) {
      const providerId = event.args.providerId;
      console.log(`✅ Service provider registered with ID: ${providerId}`);
      return providerId;
    } else {
      console.log('✅ Registration transaction successful');
      // Fetch provider ID
      const providerId = await registry.addressToProviderId(spWallet.address);
      console.log(`✅ Service provider ID: ${providerId}`);
      return providerId;
    }
  } catch (error) {
    console.error('❌ Registration failed:', error.message);

    // Try to decode revert reason
    if (error.data) {
      try {
        const decodedError = registry.interface.parseError(error.data);
        console.error('❌ Revert reason:', decodedError.name, decodedError.args);
      } catch {
        console.error('❌ Raw error data:', error.data);
      }
    }

    throw error;
  }
}

// Main execution
async function main() {
  try {
    const providerId = await registerServiceProvider();

    console.log('');
    console.log('✅ Service provider registration complete!');
    console.log(`   Provider ID: ${providerId}`);
    console.log(`   Address: ${spWallet.address}`);
    console.log(`   Registry: ${SP_REGISTRY_ADDRESS}`);
    console.log('');

    process.exit(0);
  } catch (error) {
    console.error('');
    console.error('❌ Registration failed:', error.message);
    console.error('');
    process.exit(1);
  }
}

main();
