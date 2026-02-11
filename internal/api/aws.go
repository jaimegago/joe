package api

import (
	"net/http"

	"github.com/jaimegago/joe/internal/adapters/aws"
)

// handleAWSEC2ListInstances lists EC2 instances from an AWS source
func (s *Server) handleAWSEC2ListInstances(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	adapter, err := s.services.Adapters.Get(sourceID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found: " + sourceID})
		return
	}

	awsAdapter, ok := adapter.(aws.AWSAdapter)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source is not an AWS adapter"})
		return
	}

	instances, err := awsAdapter.ListEC2Instances(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"instances": instances,
		"count":     len(instances),
		"source_id": sourceID,
	})
}

// handleAWSEC2GetInstance gets a specific EC2 instance from an AWS source
func (s *Server) handleAWSEC2GetInstance(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")
	instanceID := r.PathValue("instanceID")

	if instanceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing instance ID"})
		return
	}

	adapter, err := s.services.Adapters.Get(sourceID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found: " + sourceID})
		return
	}

	awsAdapter, ok := adapter.(aws.AWSAdapter)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source is not an AWS adapter"})
		return
	}

	instance, err := awsAdapter.GetEC2Instance(r.Context(), instanceID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if instance == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "instance not found: " + instanceID})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"instance":  instance,
		"source_id": sourceID,
	})
}

// handleAWSEKSListClusters lists EKS clusters from an AWS source
func (s *Server) handleAWSEKSListClusters(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	adapter, err := s.services.Adapters.Get(sourceID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found: " + sourceID})
		return
	}

	awsAdapter, ok := adapter.(aws.AWSAdapter)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source is not an AWS adapter"})
		return
	}

	clusters, err := awsAdapter.ListEKSClusters(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"clusters":  clusters,
		"count":     len(clusters),
		"source_id": sourceID,
	})
}

// handleAWSEKSGetCluster gets a specific EKS cluster from an AWS source
func (s *Server) handleAWSEKSGetCluster(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")
	clusterName := r.PathValue("clusterName")

	if clusterName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing cluster name"})
		return
	}

	adapter, err := s.services.Adapters.Get(sourceID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found: " + sourceID})
		return
	}

	awsAdapter, ok := adapter.(aws.AWSAdapter)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source is not an AWS adapter"})
		return
	}

	cluster, err := awsAdapter.GetEKSCluster(r.Context(), clusterName)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if cluster == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "cluster not found: " + clusterName})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"cluster":   cluster,
		"source_id": sourceID,
	})
}

// handleAWSRDSListInstances lists RDS instances from an AWS source
func (s *Server) handleAWSRDSListInstances(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	adapter, err := s.services.Adapters.Get(sourceID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found: " + sourceID})
		return
	}

	awsAdapter, ok := adapter.(aws.AWSAdapter)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source is not an AWS adapter"})
		return
	}

	instances, err := awsAdapter.ListRDSInstances(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"instances": instances,
		"count":     len(instances),
		"source_id": sourceID,
	})
}

// handleAWSRDSGetInstance gets a specific RDS instance from an AWS source
func (s *Server) handleAWSRDSGetInstance(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")
	dbInstanceID := r.PathValue("dbInstanceID")

	if dbInstanceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing DB instance ID"})
		return
	}

	adapter, err := s.services.Adapters.Get(sourceID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found: " + sourceID})
		return
	}

	awsAdapter, ok := adapter.(aws.AWSAdapter)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source is not an AWS adapter"})
		return
	}

	instance, err := awsAdapter.GetRDSInstance(r.Context(), dbInstanceID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if instance == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "DB instance not found: " + dbInstanceID})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"instance":  instance,
		"source_id": sourceID,
	})
}

// handleAWSVPCListVPCs lists VPCs from an AWS source
func (s *Server) handleAWSVPCListVPCs(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	adapter, err := s.services.Adapters.Get(sourceID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found: " + sourceID})
		return
	}

	awsAdapter, ok := adapter.(aws.AWSAdapter)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source is not an AWS adapter"})
		return
	}

	vpcs, err := awsAdapter.ListVPCs(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"vpcs":      vpcs,
		"count":     len(vpcs),
		"source_id": sourceID,
	})
}

// handleAWSVPCGetVPC gets a specific VPC from an AWS source
func (s *Server) handleAWSVPCGetVPC(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")
	vpcID := r.PathValue("vpcID")

	if vpcID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing VPC ID"})
		return
	}

	adapter, err := s.services.Adapters.Get(sourceID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found: " + sourceID})
		return
	}

	awsAdapter, ok := adapter.(aws.AWSAdapter)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source is not an AWS adapter"})
		return
	}

	vpc, err := awsAdapter.GetVPC(r.Context(), vpcID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if vpc == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "VPC not found: " + vpcID})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"vpc":       vpc,
		"source_id": sourceID,
	})
}
