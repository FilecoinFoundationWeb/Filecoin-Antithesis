package test

import (
	"fmt"
	"testing"

	big_type "github.com/filecoin-project/go-state-types/big"
	"github.com/test-go/testify/assert"
	"github.com/test-go/testify/require"
)

func TestMessageBuilder(t *testing.T) {
	// fetch powertable from lotus API, by adding context and tipset which canbe found using chainhead
	pt := NewPowerTable()
	err := pt.Add([]PowerEntry{
		{
			ID:     0,
			PubKey: PubKey{0},
			Power:  big_type.NewInt(1),
		},
		{
			ID:     1,
			PubKey: PubKey{1},
			Power:  big_type.NewInt(1),
		},
	}...)
	assert.NoError(t, err)
	// main go f3 fix
	payload := Payload{
		Instance: 1,
		Round:    0,
	}
	//get network name from lotus api api.get manifest and get manifest.networkname
	nn := NetworkName("test")

	mt := &MessageBuilder{
		NetworkName:      nn,
		PowerTable:       pt,
		Payload:          payload,
		SigningMarshaler: signingMarshaler,
	}

	// should be adding miner address, we can hard code it t01000 or something
	_, err = mt.PrepareSigningInputs(2)
	require.Error(t, err, "unknown ID should return an error")

	st, err := mt.PrepareSigningInputs(0)
	require.NoError(t, err)

	require.Equal(t, st.Payload, payload)
	require.Equal(t, st.ParticipantID, ActorID(0))
	require.Equal(t, st.PubKey, PubKey{0})
	require.NotNil(t, st.PayloadToSign)
	require.Nil(t, st.VRFToSign)

	mt.PrepareSigningInputs(1)
	require.NoError(t, err)

	require.Equal(t, st.Payload, payload)
	require.Equal(t, st.ParticipantID, ActorID(1))
	require.Equal(t, st.PubKey, PubKey{1})
	require.NotNil(t, st.PayloadToSign)
	require.Nil(t, st.VRFToSign)
	fmt.Print(mt)
}
