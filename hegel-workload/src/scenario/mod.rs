pub mod actions;
pub mod types;

use types::*;

/// All possible action kinds the scenario stepper can draw.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ActionKind {
    // Always available (no inputs required)
    PickWallet,
    ObserveChainHead,
    ObserveMempool,
    CreateFuzzedMsg,
    CreateFuzzedBlock,
    Pause,

    // Requires wallet_states (observed nonce)
    ObserveNonce,
    CreateValidTransfer,
    CreateNonceGap,
    CreateSemiValidMsg,
    CreateDrain,

    // Requires signed_msgs
    PublishMsgP2p,
    PublishMsgRpc,
    CreateNonceReuse,
    CreateGasBump,
    WaitForInclusion,

    // Requires fuzzed_blocks
    PublishBlockP2p,

    // Requires chain_tips
    CreateBlockAtHeight,
}

/// Holds the typed bag of intermediate values produced by actions.
pub struct ScenarioContext {
    pub wallets: Vec<Wallet>,
    pub wallet_states: Vec<WalletState>,
    pub signed_msgs: Vec<SignedMsg>,
    pub fuzzed_blocks: Vec<FuzzedBlock>,
    pub chain_tips: Vec<ChainTip>,
    pub mempool_snapshots: Vec<MempoolSnapshot>,
}

impl ScenarioContext {
    pub fn new(wallets: Vec<Wallet>) -> Self {
        Self {
            wallets,
            wallet_states: vec![],
            signed_msgs: vec![],
            fuzzed_blocks: vec![],
            chain_tips: vec![],
            mempool_snapshots: vec![],
        }
    }

    /// Return the list of actions whose input requirements are satisfied.
    pub fn available_actions(&self) -> Vec<ActionKind> {
        let mut actions = vec![
            ActionKind::PickWallet,
            ActionKind::ObserveChainHead,
            ActionKind::ObserveMempool,
            ActionKind::CreateFuzzedMsg,
            ActionKind::CreateFuzzedBlock,
            ActionKind::Pause,
        ];

        if !self.wallet_states.is_empty() {
            actions.extend_from_slice(&[
                ActionKind::ObserveNonce,
                ActionKind::CreateValidTransfer,
                ActionKind::CreateNonceGap,
                ActionKind::CreateSemiValidMsg,
                ActionKind::CreateDrain,
            ]);
        }

        if !self.signed_msgs.is_empty() {
            actions.extend_from_slice(&[
                ActionKind::PublishMsgP2p,
                ActionKind::PublishMsgRpc,
                ActionKind::CreateNonceReuse,
                ActionKind::CreateGasBump,
                ActionKind::WaitForInclusion,
            ]);
        }

        if !self.fuzzed_blocks.is_empty() {
            actions.push(ActionKind::PublishBlockP2p);
        }

        if !self.chain_tips.is_empty() {
            actions.push(ActionKind::CreateBlockAtHeight);
        }

        actions
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use fvm_shared::address::Address;

    fn empty_context() -> ScenarioContext {
        ScenarioContext::new(vec![Wallet {
            address: Address::new_id(1000),
            private_key: vec![1u8; 32],
        }])
    }

    fn dummy_signed_msg() -> SignedMsg {
        use fvm_shared::econ::TokenAmount;
        use fvm_ipld_encoding::RawBytes;
        SignedMsg {
            message: fvm_shared::message::Message {
                version: 0,
                to: Address::new_id(1000),
                from: Address::new_id(1001),
                sequence: 0,
                value: TokenAmount::from_atto(0u64),
                method_num: 0,
                params: RawBytes::new(vec![]),
                gas_limit: 0,
                gas_fee_cap: TokenAmount::from_atto(0u64),
                gas_premium: TokenAmount::from_atto(0u64),
            },
            signature: fvm_shared::crypto::signature::Signature::new_secp256k1(vec![0u8; 65]),
            cbor_bytes: vec![],
            sender_key: vec![1u8; 32],
        }
    }

    #[test]
    fn test_initial_actions_available() {
        let ctx = empty_context();
        let actions = ctx.available_actions();
        assert!(actions.contains(&ActionKind::PickWallet));
        assert!(actions.contains(&ActionKind::ObserveChainHead));
        assert!(actions.contains(&ActionKind::ObserveMempool));
        assert!(actions.contains(&ActionKind::CreateFuzzedMsg));
        assert!(actions.contains(&ActionKind::CreateFuzzedBlock));
        assert!(actions.contains(&ActionKind::Pause));
        // These require inputs we don't have yet
        assert!(!actions.contains(&ActionKind::CreateValidTransfer));
        assert!(!actions.contains(&ActionKind::PublishMsgP2p));
    }

    #[test]
    fn test_transfer_available_after_observe() {
        let mut ctx = empty_context();
        ctx.wallet_states.push(WalletState {
            wallet: ctx.wallets[0].clone(),
            nonce: 0,
        });
        let actions = ctx.available_actions();
        assert!(actions.contains(&ActionKind::CreateValidTransfer));
        assert!(actions.contains(&ActionKind::CreateNonceGap));
        assert!(actions.contains(&ActionKind::CreateSemiValidMsg));
    }

    #[test]
    fn test_publish_available_after_create() {
        let mut ctx = empty_context();
        ctx.signed_msgs.push(dummy_signed_msg());
        let actions = ctx.available_actions();
        assert!(actions.contains(&ActionKind::PublishMsgP2p));
        assert!(actions.contains(&ActionKind::PublishMsgRpc));
        assert!(actions.contains(&ActionKind::CreateNonceReuse));
        assert!(actions.contains(&ActionKind::CreateGasBump));
    }

    #[test]
    fn test_block_publish_available() {
        let mut ctx = empty_context();
        ctx.fuzzed_blocks.push(FuzzedBlock {
            cbor_bytes: vec![0x83],
        });
        let actions = ctx.available_actions();
        assert!(actions.contains(&ActionKind::PublishBlockP2p));
    }

    #[test]
    fn test_block_at_height_requires_chain_tip() {
        let mut ctx = empty_context();
        assert!(!ctx.available_actions().contains(&ActionKind::CreateBlockAtHeight));
        ctx.chain_tips.push(ChainTip { height: 100 });
        assert!(ctx.available_actions().contains(&ActionKind::CreateBlockAtHeight));
    }
}
