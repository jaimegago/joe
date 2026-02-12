package api

import (
	"fmt"
	"net/http"
	"time"
)

const (
	// Error messages for AWS API handlers
	errorMissingInstanceID   = "missing instance ID"
	errorMissingClusterName  = "missing cluster name"
	errorMissingDBInstanceID = "missing database instance ID"
	errorMissingVPCID        = "missing VPC ID"
	errorInstanceNotFound    = "instance not found"
	errorClusterNotFound     = "cluster not found"
	errorDBInstanceNotFound  = "database instance not found"
	errorVPCNotFound         = "VPC not found"
)

// handleAWSEC2ListInstances lists EC2 instances from an AWS source
func (s *Server) handleAWSEC2ListInstances(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	awsAdapter, err := s.getAWSAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "AWS") {
		return
	}

	start := time.Now()
	instances, err := awsAdapter.ListEC2Instances(r.Context())
	s.services.Metrics.RecordAdapterCall(r.Context(), "aws", "ec2.list_instances", time.Since(start), err)
	if err != nil {
		writeInternalError(w, err, "aws ec2 list instances")
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
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, errorMissingInstanceID)
		return
	}

	awsAdapter, err := s.getAWSAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "AWS") {
		return
	}

	start := time.Now()
	instance, err := awsAdapter.GetEC2Instance(r.Context(), instanceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "aws", "ec2.get_instance", time.Since(start), err)
	if err != nil {
		writeInternalError(w, err, "aws ec2 get instance")
		return
	}

	if instance == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, fmt.Sprintf("%s: %s", errorInstanceNotFound, instanceID))
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

	awsAdapter, err := s.getAWSAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "AWS") {
		return
	}

	start := time.Now()
	clusters, err := awsAdapter.ListEKSClusters(r.Context())
	s.services.Metrics.RecordAdapterCall(r.Context(), "aws", "eks.list_clusters", time.Since(start), err)
	if err != nil {
		writeInternalError(w, err, "aws eks list clusters")
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
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, errorMissingClusterName)
		return
	}

	awsAdapter, err := s.getAWSAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "AWS") {
		return
	}

	start := time.Now()
	cluster, err := awsAdapter.GetEKSCluster(r.Context(), clusterName)
	s.services.Metrics.RecordAdapterCall(r.Context(), "aws", "eks.get_cluster", time.Since(start), err)
	if err != nil {
		writeInternalError(w, err, "aws eks get cluster")
		return
	}

	if cluster == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, fmt.Sprintf("%s: %s", errorClusterNotFound, clusterName))
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

	awsAdapter, err := s.getAWSAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "AWS") {
		return
	}

	start := time.Now()
	instances, err := awsAdapter.ListRDSInstances(r.Context())
	s.services.Metrics.RecordAdapterCall(r.Context(), "aws", "rds.list_instances", time.Since(start), err)
	if err != nil {
		writeInternalError(w, err, "aws rds list instances")
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
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, errorMissingDBInstanceID)
		return
	}

	awsAdapter, err := s.getAWSAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "AWS") {
		return
	}

	start := time.Now()
	instance, err := awsAdapter.GetRDSInstance(r.Context(), dbInstanceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "aws", "rds.get_instance", time.Since(start), err)
	if err != nil {
		writeInternalError(w, err, "aws rds get instance")
		return
	}

	if instance == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, fmt.Sprintf("%s: %s", errorDBInstanceNotFound, dbInstanceID))
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

	awsAdapter, err := s.getAWSAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "AWS") {
		return
	}

	start := time.Now()
	vpcs, err := awsAdapter.ListVPCs(r.Context())
	s.services.Metrics.RecordAdapterCall(r.Context(), "aws", "vpc.list", time.Since(start), err)
	if err != nil {
		writeInternalError(w, err, "aws vpc list vpcs")
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
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, errorMissingVPCID)
		return
	}

	awsAdapter, err := s.getAWSAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "AWS") {
		return
	}

	start := time.Now()
	vpc, err := awsAdapter.GetVPC(r.Context(), vpcID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "aws", "vpc.get", time.Since(start), err)
	if err != nil {
		writeInternalError(w, err, "aws vpc get vpc")
		return
	}

	if vpc == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, fmt.Sprintf("%s: %s", errorVPCNotFound, vpcID))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"vpc":       vpc,
		"source_id": sourceID,
	})
}
