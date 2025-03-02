package scalers

/*
Copyright 2021 The KEDA Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"

	eventhub "github.com/Azure/azure-event-hubs-go/v3"
	"github.com/Azure/azure-storage-blob-go/azblob"
	az "github.com/Azure/go-autorest/autorest/azure"
	"k8s.io/api/autoscaling/v2beta2"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/metrics/pkg/apis/external_metrics"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kedacore/keda/v2/apis/keda/v1alpha1"
	"github.com/kedacore/keda/v2/pkg/scalers/azure"
	kedautil "github.com/kedacore/keda/v2/pkg/util"
)

const (
	defaultEventHubMessageThreshold = 64
	eventHubMetricType              = "External"
	thresholdMetricName             = "unprocessedEventThreshold"
	defaultEventHubConsumerGroup    = "$Default"
	defaultBlobContainer            = ""
	defaultCheckpointStrategy       = ""
)

var eventhubLog = logf.Log.WithName("azure_eventhub_scaler")

type azureEventHubScaler struct {
	metricType v2beta2.MetricTargetType
	metadata   *eventHubMetadata
	client     *eventhub.Hub
	httpClient *http.Client
}

type eventHubMetadata struct {
	eventHubInfo azure.EventHubInfo
	threshold    int64
	scalerIndex  int
}

// NewAzureEventHubScaler creates a new scaler for eventHub
func NewAzureEventHubScaler(ctx context.Context, config *ScalerConfig) (Scaler, error) {
	metricType, err := GetMetricTargetType(config)
	if err != nil {
		return nil, fmt.Errorf("error getting scaler metric type: %s", err)
	}

	parsedMetadata, err := parseAzureEventHubMetadata(config)
	if err != nil {
		return nil, fmt.Errorf("unable to get eventhub metadata: %s", err)
	}

	hub, err := azure.GetEventHubClient(ctx, parsedMetadata.eventHubInfo)
	if err != nil {
		return nil, fmt.Errorf("unable to get eventhub client: %s", err)
	}

	return &azureEventHubScaler{
		metricType: metricType,
		metadata:   parsedMetadata,
		client:     hub,
		httpClient: kedautil.CreateHTTPClient(config.GlobalHTTPTimeout, false),
	}, nil
}

// parseAzureEventHubMetadata parses metadata
func parseAzureEventHubMetadata(config *ScalerConfig) (*eventHubMetadata, error) {
	meta := eventHubMetadata{
		eventHubInfo: azure.EventHubInfo{},
	}
	meta.threshold = defaultEventHubMessageThreshold

	if val, ok := config.TriggerMetadata[thresholdMetricName]; ok {
		threshold, err := kedautil.ParseNumeric(val, 64, false)
		if err != nil {
			return nil, fmt.Errorf("error parsing azure eventhub metadata %s: %s", thresholdMetricName, err)
		}
		typedValue, ok := threshold.(int64)
		if !ok {
			return nil, fmt.Errorf("provided value for threshold (%d) was not a valid integer", threshold)
		}

		meta.threshold = typedValue
	}

	if config.AuthParams["storageConnection"] != "" {
		meta.eventHubInfo.StorageConnection = config.AuthParams["storageConnection"]
	} else if config.TriggerMetadata["storageConnectionFromEnv"] != "" {
		meta.eventHubInfo.StorageConnection = config.ResolvedEnv[config.TriggerMetadata["storageConnectionFromEnv"]]
	}

	if len(meta.eventHubInfo.StorageConnection) == 0 {
		return nil, fmt.Errorf("no storage connection string given")
	}

	meta.eventHubInfo.EventHubConsumerGroup = defaultEventHubConsumerGroup
	if val, ok := config.TriggerMetadata["consumerGroup"]; ok {
		meta.eventHubInfo.EventHubConsumerGroup = val
	}

	meta.eventHubInfo.CheckpointStrategy = defaultCheckpointStrategy
	if val, ok := config.TriggerMetadata["checkpointStrategy"]; ok {
		meta.eventHubInfo.CheckpointStrategy = val
	}

	meta.eventHubInfo.BlobContainer = defaultBlobContainer
	if val, ok := config.TriggerMetadata["blobContainer"]; ok {
		meta.eventHubInfo.BlobContainer = val
	}

	meta.eventHubInfo.EventHubResourceURL = azure.DefaultEventhubResourceURL
	if val, ok := config.TriggerMetadata["cloud"]; ok {
		if strings.EqualFold(val, azure.PrivateCloud) {
			if resourceURL, ok := config.TriggerMetadata["eventHubResourceURL"]; ok {
				meta.eventHubInfo.EventHubResourceURL = resourceURL
			} else {
				return nil, fmt.Errorf("eventHubResourceURL must be provided for %s cloud type", azure.PrivateCloud)
			}
		}
	}

	serviceBusEndpointSuffixProvider := func(env az.Environment) (string, error) {
		return env.ServiceBusEndpointSuffix, nil
	}
	serviceBusEndpointSuffix, err := azure.ParseEnvironmentProperty(config.TriggerMetadata, azure.DefaultEndpointSuffixKey, serviceBusEndpointSuffixProvider)
	if err != nil {
		return nil, err
	}
	meta.eventHubInfo.ServiceBusEndpointSuffix = serviceBusEndpointSuffix

	activeDirectoryEndpoint, err := azure.ParseActiveDirectoryEndpoint(config.TriggerMetadata)
	if err != nil {
		return nil, err
	}
	meta.eventHubInfo.ActiveDirectoryEndpoint = activeDirectoryEndpoint

	meta.eventHubInfo.PodIdentity = config.PodIdentity
	switch config.PodIdentity {
	case "", v1alpha1.PodIdentityProviderNone:
		if config.AuthParams["connection"] != "" {
			meta.eventHubInfo.EventHubConnection = config.AuthParams["connection"]
		} else if config.TriggerMetadata["connectionFromEnv"] != "" {
			meta.eventHubInfo.EventHubConnection = config.ResolvedEnv[config.TriggerMetadata["connectionFromEnv"]]
		}

		if len(meta.eventHubInfo.EventHubConnection) == 0 {
			return nil, fmt.Errorf("no event hub connection string given")
		}
	case v1alpha1.PodIdentityProviderAzure, v1alpha1.PodIdentityProviderAzureWorkload:
		if config.TriggerMetadata["eventHubNamespace"] != "" {
			meta.eventHubInfo.Namespace = config.TriggerMetadata["eventHubNamespace"]
		} else if config.TriggerMetadata["eventHubNamespaceFromEnv"] != "" {
			meta.eventHubInfo.Namespace = config.ResolvedEnv[config.TriggerMetadata["eventHubNamespaceFromEnv"]]
		}

		if len(meta.eventHubInfo.Namespace) == 0 {
			return nil, fmt.Errorf("no event hub namespace string given")
		}

		if config.TriggerMetadata["eventHubName"] != "" {
			meta.eventHubInfo.EventHubName = config.TriggerMetadata["eventHubName"]
		} else if config.TriggerMetadata["eventHubNameFromEnv"] != "" {
			meta.eventHubInfo.EventHubName = config.ResolvedEnv[config.TriggerMetadata["eventHubNameFromEnv"]]
		}

		if len(meta.eventHubInfo.EventHubName) == 0 {
			return nil, fmt.Errorf("no event hub name string given")
		}
	}

	meta.scalerIndex = config.ScalerIndex

	return &meta, nil
}

// GetUnprocessedEventCountInPartition gets number of unprocessed events in a given partition
func (scaler *azureEventHubScaler) GetUnprocessedEventCountInPartition(ctx context.Context, partitionInfo *eventhub.HubPartitionRuntimeInformation) (newEventCount int64, checkpoint azure.Checkpoint, err error) {
	// if partitionInfo.LastEnqueuedOffset = -1, that means event hub partition is empty
	if partitionInfo != nil && partitionInfo.LastEnqueuedOffset == "-1" {
		return 0, azure.Checkpoint{}, nil
	}

	checkpoint, err = azure.GetCheckpointFromBlobStorage(ctx, scaler.httpClient, scaler.metadata.eventHubInfo, partitionInfo.PartitionID)
	if err != nil {
		// if blob not found return the total partition event count
		err = errors.Unwrap(err)
		if stErr, ok := err.(azblob.StorageError); ok {
			if stErr.ServiceCode() == azblob.ServiceCodeBlobNotFound || stErr.ServiceCode() == azblob.ServiceCodeContainerNotFound {
				eventhubLog.V(1).Error(err, fmt.Sprintf("Blob container : %s not found to use checkpoint strategy, getting unprocessed event count without checkpoint", scaler.metadata.eventHubInfo.BlobContainer))
				return GetUnprocessedEventCountWithoutCheckpoint(partitionInfo), azure.Checkpoint{}, nil
			}
		}
		return -1, azure.Checkpoint{}, fmt.Errorf("unable to get checkpoint from storage: %s", err)
	}

	unprocessedEventCountInPartition := int64(0)

	// If checkpoint.Offset is empty that means no messages has been processed from an event hub partition
	// And since partitionInfo.LastSequenceNumber = 0 for the very first message hence
	// total unprocessed message will be partitionInfo.LastSequenceNumber + 1
	if checkpoint.Offset == "" {
		unprocessedEventCountInPartition = partitionInfo.LastSequenceNumber + 1
		return unprocessedEventCountInPartition, checkpoint, nil
	}

	if partitionInfo.LastSequenceNumber >= checkpoint.SequenceNumber {
		unprocessedEventCountInPartition = partitionInfo.LastSequenceNumber - checkpoint.SequenceNumber
		return unprocessedEventCountInPartition, checkpoint, nil
	}

	// Partition is a circular buffer, so it is possible that
	// partitionInfo.LastSequenceNumber < blob checkpoint's SequenceNumber
	unprocessedEventCountInPartition = (math.MaxInt64 - partitionInfo.LastSequenceNumber) + checkpoint.SequenceNumber

	// Checkpointing may or may not be always behind partition's LastSequenceNumber.
	// The partition information read could be stale compared to checkpoint,
	// especially when load is very small and checkpointing is happening often.
	// e.g., (9223372036854775807 - 10) + 11 = -9223372036854775808
	// If unprocessedEventCountInPartition is negative that means there are 0 unprocessed messages in the partition
	if unprocessedEventCountInPartition < 0 {
		unprocessedEventCountInPartition = 0
	}

	return unprocessedEventCountInPartition, checkpoint, nil
}

// GetUnprocessedEventCountWithoutCheckpoint returns the number of messages on the without a checkoutpoint info
func GetUnprocessedEventCountWithoutCheckpoint(partitionInfo *eventhub.HubPartitionRuntimeInformation) int64 {
	// if both values are 0 then there is exactly one message inside the hub. First message after init
	if (partitionInfo.BeginningSequenceNumber == 0 && partitionInfo.LastSequenceNumber == 0) || (partitionInfo.BeginningSequenceNumber != partitionInfo.LastSequenceNumber) {
		return (partitionInfo.LastSequenceNumber - partitionInfo.BeginningSequenceNumber) + 1
	}

	return 0
}

// IsActive determines if eventhub is active based on number of unprocessed events
func (scaler *azureEventHubScaler) IsActive(ctx context.Context) (bool, error) {
	runtimeInfo, err := scaler.client.GetRuntimeInformation(ctx)
	if err != nil {
		eventhubLog.Error(err, "unable to get runtimeInfo for isActive")
		return false, fmt.Errorf("unable to get runtimeInfo for isActive: %s", err)
	}

	partitionIDs := runtimeInfo.PartitionIDs

	for i := 0; i < len(partitionIDs); i++ {
		partitionID := partitionIDs[i]

		partitionRuntimeInfo, err := scaler.client.GetPartitionInformation(ctx, partitionID)
		if err != nil {
			return false, fmt.Errorf("unable to get partitionRuntimeInfo for metrics: %s", err)
		}

		unprocessedEventCount, _, err := scaler.GetUnprocessedEventCountInPartition(ctx, partitionRuntimeInfo)

		if err != nil {
			return false, fmt.Errorf("unable to get unprocessedEventCount for isActive: %s", err)
		}

		if unprocessedEventCount > 0 {
			return true, nil
		}
	}

	return false, nil
}

// GetMetricSpecForScaling returns metric spec
func (scaler *azureEventHubScaler) GetMetricSpecForScaling(context.Context) []v2beta2.MetricSpec {
	externalMetric := &v2beta2.ExternalMetricSource{
		Metric: v2beta2.MetricIdentifier{
			Name: GenerateMetricNameWithIndex(scaler.metadata.scalerIndex, kedautil.NormalizeString(fmt.Sprintf("azure-eventhub-%s", scaler.metadata.eventHubInfo.EventHubConsumerGroup))),
		},
		Target: GetMetricTarget(scaler.metricType, scaler.metadata.threshold),
	}
	metricSpec := v2beta2.MetricSpec{External: externalMetric, Type: eventHubMetricType}
	return []v2beta2.MetricSpec{metricSpec}
}

// GetMetrics returns metric using total number of unprocessed events in event hub
func (scaler *azureEventHubScaler) GetMetrics(ctx context.Context, metricName string, metricSelector labels.Selector) ([]external_metrics.ExternalMetricValue, error) {
	totalUnprocessedEventCount := int64(0)
	runtimeInfo, err := scaler.client.GetRuntimeInformation(ctx)
	if err != nil {
		return []external_metrics.ExternalMetricValue{}, fmt.Errorf("unable to get runtimeInfo for metrics: %s", err)
	}

	partitionIDs := runtimeInfo.PartitionIDs

	for i := 0; i < len(partitionIDs); i++ {
		partitionID := partitionIDs[i]
		partitionRuntimeInfo, err := scaler.client.GetPartitionInformation(ctx, partitionID)
		if err != nil {
			return []external_metrics.ExternalMetricValue{}, fmt.Errorf("unable to get partitionRuntimeInfo for metrics: %s", err)
		}

		unprocessedEventCount := int64(0)

		unprocessedEventCount, checkpoint, err := scaler.GetUnprocessedEventCountInPartition(ctx, partitionRuntimeInfo)
		if err != nil {
			return []external_metrics.ExternalMetricValue{}, fmt.Errorf("unable to get unprocessedEventCount for metrics: %s", err)
		}

		totalUnprocessedEventCount += unprocessedEventCount

		eventhubLog.V(1).Info(fmt.Sprintf("Partition ID: %s, Last Enqueued Offset: %s, Checkpoint Offset: %s, Total new events in partition: %d",
			partitionRuntimeInfo.PartitionID, partitionRuntimeInfo.LastEnqueuedOffset, checkpoint.Offset, unprocessedEventCount))
	}

	// don't scale out beyond the number of partitions
	lagRelatedToPartitionCount := getTotalLagRelatedToPartitionAmount(totalUnprocessedEventCount, int64(len(partitionIDs)), scaler.metadata.threshold)

	eventhubLog.V(1).Info(fmt.Sprintf("Unprocessed events in event hub total: %d, scaling for a lag of %d related to %d partitions", totalUnprocessedEventCount, lagRelatedToPartitionCount, len(partitionIDs)))

	metric := external_metrics.ExternalMetricValue{
		MetricName: metricName,
		Value:      *resource.NewQuantity(lagRelatedToPartitionCount, resource.DecimalSI),
		Timestamp:  metav1.Now(),
	}

	return append([]external_metrics.ExternalMetricValue{}, metric), nil
}

func getTotalLagRelatedToPartitionAmount(unprocessedEventsCount int64, partitionCount int64, threshold int64) int64 {
	if (unprocessedEventsCount / threshold) > partitionCount {
		return partitionCount * threshold
	}

	return unprocessedEventsCount
}

// Close closes Azure Event Hub Scaler
func (scaler *azureEventHubScaler) Close(ctx context.Context) error {
	if scaler.client != nil {
		err := scaler.client.Close(ctx)
		if err != nil {
			eventhubLog.Error(err, "error closing azure event hub client")
			return err
		}
	}

	return nil
}
