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
const FWSS_ADDRESS = args['warm-storage'] || process.env.FWSS_PROXY_ADDRESS;
const SP_REGISTRY_ADDRESS = args['sp-registry'] || process.env.SERVICE_PROVIDER_REGISTRY_PROXY_ADDRESS;

// Environment variables
const DEPLOYER_PRIVATE_KEY = process.env.DEPLOYER_PRIVATE_KEY;
const SP_PRIVATE_KEY = process.env.SP_PRIVATE_KEY;

console.log('ℹ️  🔥 Adding service provider to warm storage global whitelist');
console.log(`ℹ️  RPC: ${RPC_URL}`);
console.log(`ℹ️  Warm Storage: ${FWSS_ADDRESS}`);
console.log(`ℹ️  SP Registry: ${SP_REGISTRY_ADDRESS}`);

// Load ABIs
const WORKSPACE_PATH = '/opt/filwizard/workspace';
const registryAbiPath = path.join(WORKSPACE_PATH, 'filecoinwarmstorage', 'service_contracts', 'abi', 'ServiceProviderRegistry.abi.json');
const fwssAbiPath = path.join(WORKSPACE_PATH, 'filecoinwarmstorage', 'service_contracts', 'abi', 'FilecoinWarmStorageService.abi.json');

let registryAbi, fwssAbi;
try {
  registryAbi = JSON.parse(fs.readFileSync(registryAbiPath, 'utf8'));
} catch (error) {
  console.error('❌ Failed to load ServiceProviderRegistry ABI:', error.message);
  console.error('❌ Path:', registryAbiPath);
  process.exit(1);
}

try {
  fwssAbi = JSON.parse(fs.readFileSync(fwssAbiPath, 'utf8'));
} catch (error) {
  console.error('❌ Failed to load FWSS ABI:', error.message);
  console.error('❌ Path:', fwssAbiPath);

  // Try to list available ABI files for debugging
  try {
    const abiDir = path.join(WORKSPACE_PATH, 'filecoinwarmstorage', 'service_contracts', 'abi');
    const files = fs.readdirSync(abiDir);
    console.error('❌ Available ABI files:', files.join(', '));
  } catch (listError) {
    console.error('❌ Could not list ABI directory');
  }

  process.exit(1);
}

// Setup provider and wallets
const provider = new ethers.JsonRpcProvider(RPC_URL);
const deployerWallet = new ethers.Wallet(DEPLOYER_PRIVATE_KEY, provider);
const spWallet = new ethers.Wallet(SP_PRIVATE_KEY, provider);

console.log(`ℹ️  Deployer (Owner): ${deployerWallet.address}`);
console.log(`ℹ️  Service Provider: ${spWallet.address}`);
console.log('ℹ️  ');

// Create contract instances (use deployer wallet - owner only operation)
const registry = new ethers.Contract(SP_REGISTRY_ADDRESS, registryAbi, deployerWallet);
const fwss = new ethers.Contract(FWSS_ADDRESS, fwssAbi, deployerWallet);

async function addApprovedProvider() {
  console.log('📋 Step 1: Getting Provider ID from Registry');

  // Get provider ID from registry
  let providerId;
  try {
    providerId = await registry.addressToProviderId(spWallet.address);

    if (providerId === 0n) {
      console.error('❌ Service provider not found in registry. Please register first.');
      process.exit(1);
    }

    console.log(`✅ Found provider ID: ${providerId}`);
  } catch (error) {
    console.error('❌ Failed to get provider ID:', error.message);
    throw error;
  }

  console.log('');
  console.log('📋 Step 2: Checking if Provider Already in Global Whitelist');

  // Check if already approved globally
  try {
    const isApproved = await fwss.isProviderApproved(providerId);

    if (isApproved) {
      console.log(`✅ Provider ${providerId} is already in the global approved list`);
      return providerId;
    }

    console.log(`ℹ️  Provider not yet in global whitelist`);
  } catch (error) {
    // If check fails, continue with approval
    console.log(`ℹ️  Could not check approval status, continuing...`);
  }

  console.log('');
  console.log('📋 Step 3: Adding Provider to Global Whitelist (Owner Operation)');

  try {
    console.log(`ℹ️  Adding provider ${providerId} to global whitelist...`);

    const tx = await fwss.addApprovedProvider(providerId);

    console.log(`ℹ️  Transaction submitted: ${tx.hash}`);
    console.log('ℹ️  Waiting for confirmation...');

    const receipt = await tx.wait();
    console.log(`✅ Transaction confirmed in block ${receipt.blockNumber}`);

    // Extract ProviderApproved event
    const event = receipt.logs
      .map(log => {
        try {
          return fwss.interface.parseLog(log);
        } catch {
          return null;
        }
      })
      .find(e => e && e.name === 'ProviderApproved');

    if (event) {
      console.log(`✅ Provider approved event emitted:`);
      console.log(`   Provider ID: ${event.args.providerId || providerId}`);
    } else {
      console.log('✅ Provider added to global whitelist successfully');
    }

    return providerId;

  } catch (error) {
    console.error('❌ Failed to add provider to global whitelist:', error.message);

    // Try to decode revert reason
    if (error.data) {
      try {
        const decodedError = fwss.interface.parseError(error.data);
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
    const providerId = await addApprovedProvider();

    console.log('');
    console.log('✅ Service provider added to global whitelist!');
    console.log(`   Provider ID: ${providerId}`);
    console.log(`   SP Address: ${spWallet.address}`);
    console.log(`   Warm Storage: ${FWSS_ADDRESS}`);
    console.log(`   Status: Available for all dapps and clients`);
    console.log('');

    process.exit(0);
  } catch (error) {
    console.error('');
    console.error('❌ Failed to add provider to global whitelist:', error.message);
    console.error('');
    process.exit(1);
  }
}

main();
