package metadataprocessor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/processor/processortest"
)

func TestCreateDefaultConfig(t *testing.T) {
	factory := NewFactory()

	cfg := factory.CreateDefaultConfig()
	assert.NotNil(t, cfg, "failed to create default config")
	assert.NoError(t, componenttest.CheckConfigStruct(cfg))
}

func TestCreateProcessor(t *testing.T) {
	factory := NewFactory()

	cfg := factory.CreateDefaultConfig()
	creationSet := processortest.NewNopSettings()

	mp, err := factory.CreateMetricsProcessor(context.Background(), creationSet, cfg, nil)
	assert.NotNil(t, mp)
	assert.NoError(t, err, "cannot create metric processor")
	assert.NoError(t, mp.Shutdown(context.Background()))
}
