// The MIT License
//
// Copyright (c) 2020 Temporal Technologies Inc.  All rights reserved.
//
// Copyright (c) 2020 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package namespace

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/gogo/protobuf/types"
	"github.com/pborman/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	enumspb "go.temporal.io/api/enums/v1"
	namespacepb "go.temporal.io/api/namespace/v1"
	replicationpb "go.temporal.io/api/replication/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflowservice/v1"

	"go.temporal.io/server/common"
	"go.temporal.io/server/common/archiver"
	"go.temporal.io/server/common/archiver/provider"
	"go.temporal.io/server/common/cluster"
	"go.temporal.io/server/common/log/loggerimpl"
	"go.temporal.io/server/common/mocks"
	"go.temporal.io/server/common/persistence"
	persistencetests "go.temporal.io/server/common/persistence/persistence-tests"
	"go.temporal.io/server/common/service/config"
	dc "go.temporal.io/server/common/service/dynamicconfig"
)

type (
	namespaceHandlerGlobalNamespaceEnabledMasterClusterSuite struct {
		suite.Suite
		persistencetests.TestBase

		minRetentionDays        int
		maxBadBinaryCount       int
		metadataMgr             persistence.MetadataManager
		mockProducer            *mocks.KafkaProducer
		mockNamespaceReplicator Replicator
		archivalMetadata        archiver.ArchivalMetadata
		mockArchiverProvider    *provider.MockArchiverProvider

		handler *HandlerImpl
	}
)

func TestNamespaceHandlerGlobalNamespaceEnabledMasterClusterSuite(t *testing.T) {
	s := new(namespaceHandlerGlobalNamespaceEnabledMasterClusterSuite)
	suite.Run(t, s)
}

func (s *namespaceHandlerGlobalNamespaceEnabledMasterClusterSuite) SetupSuite() {
	if testing.Verbose() {
		log.SetOutput(os.Stdout)
	}

	s.TestBase = persistencetests.NewTestBaseWithCassandra(&persistencetests.TestBaseOptions{
		ClusterMetadata: cluster.GetTestClusterMetadata(true, true),
	})
	s.TestBase.Setup()
}

func (s *namespaceHandlerGlobalNamespaceEnabledMasterClusterSuite) TearDownSuite() {
	s.TestBase.TearDownWorkflowStore()
}

func (s *namespaceHandlerGlobalNamespaceEnabledMasterClusterSuite) SetupTest() {
	logger := loggerimpl.NewNopLogger()
	dcCollection := dc.NewCollection(dc.NewNopClient(), logger)
	s.minRetentionDays = 1
	s.maxBadBinaryCount = 10
	s.metadataMgr = s.TestBase.MetadataManager
	s.mockProducer = &mocks.KafkaProducer{}
	s.mockNamespaceReplicator = NewNamespaceReplicator(s.mockProducer, logger)
	s.archivalMetadata = archiver.NewArchivalMetadata(
		dcCollection,
		"",
		false,
		"",
		false,
		&config.ArchivalNamespaceDefaults{},
	)
	s.mockArchiverProvider = &provider.MockArchiverProvider{}
	s.handler = NewHandler(
		s.minRetentionDays,
		dc.GetIntPropertyFilteredByNamespace(s.maxBadBinaryCount),
		logger,
		s.metadataMgr,
		s.ClusterMetadata,
		s.mockNamespaceReplicator,
		s.archivalMetadata,
		s.mockArchiverProvider,
	)
}

func (s *namespaceHandlerGlobalNamespaceEnabledMasterClusterSuite) TearDownTest() {
	s.mockProducer.AssertExpectations(s.T())
	s.mockArchiverProvider.AssertExpectations(s.T())
}

func (s *namespaceHandlerGlobalNamespaceEnabledMasterClusterSuite) TestRegisterGetNamespace_LocalNamespace_InvalidCluster() {
	namespace := s.getRandomNamespace()
	description := "some random description"
	email := "some random email"
	retention := int32(7)
	emitMetric := true
	activeClusterName := cluster.TestAlternativeClusterName
	clusters := []*replicationpb.ClusterReplicationConfig{
		{
			ClusterName: activeClusterName,
		},
	}
	data := map[string]string{"some random key": "some random value"}
	isGlobalNamespace := false

	registerResp, err := s.handler.RegisterNamespace(context.Background(), &workflowservice.RegisterNamespaceRequest{
		Name:                                 namespace,
		Description:                          description,
		OwnerEmail:                           email,
		WorkflowExecutionRetentionPeriodDays: retention,
		EmitMetric:                           emitMetric,
		Clusters:                             clusters,
		ActiveClusterName:                    activeClusterName,
		Data:                                 data,
		IsGlobalNamespace:                    isGlobalNamespace,
	})
	s.Error(err)
	s.IsType(&serviceerror.InvalidArgument{}, err)
	s.Nil(registerResp)
}

func (s *namespaceHandlerGlobalNamespaceEnabledMasterClusterSuite) TestRegisterGetNamespace_LocalNamespace_AllDefault() {
	namespace := s.getRandomNamespace()
	isGlobalNamespace := false
	var clusters []*replicationpb.ClusterReplicationConfig
	for _, name := range persistence.GetOrUseDefaultClusters(s.ClusterMetadata.GetCurrentClusterName(), nil) {
		clusters = append(clusters, &replicationpb.ClusterReplicationConfig{
			ClusterName: name,
		})
	}

	retention := int32(1)
	registerResp, err := s.handler.RegisterNamespace(context.Background(), &workflowservice.RegisterNamespaceRequest{
		Name:                                 namespace,
		IsGlobalNamespace:                    isGlobalNamespace,
		WorkflowExecutionRetentionPeriodDays: retention,
	})
	s.NoError(err)
	s.Nil(registerResp)

	resp, err := s.handler.DescribeNamespace(context.Background(), &workflowservice.DescribeNamespaceRequest{
		Name: namespace,
	})
	s.NoError(err)

	s.NotEmpty(resp.NamespaceInfo.GetId())
	resp.NamespaceInfo.Id = ""
	s.Equal(&namespacepb.NamespaceInfo{
		Name:        namespace,
		State:       enumspb.NAMESPACE_STATE_REGISTERED,
		Description: "",
		OwnerEmail:  "",
		Data:        map[string]string{},
		Id:          "",
	}, resp.NamespaceInfo)
	s.Equal(&namespacepb.NamespaceConfig{
		WorkflowExecutionRetentionPeriodInDays: retention,
		EmitMetric:                             &types.BoolValue{Value: false},
		HistoryArchivalState:                   enumspb.ARCHIVAL_STATE_DISABLED,
		HistoryArchivalUri:                     "",
		VisibilityArchivalState:                enumspb.ARCHIVAL_STATE_DISABLED,
		VisibilityArchivalUri:                  "",
		BadBinaries:                            &namespacepb.BadBinaries{Binaries: map[string]*namespacepb.BadBinaryInfo{}},
	}, resp.Config)
	s.Equal(&replicationpb.NamespaceReplicationConfig{
		ActiveClusterName: s.ClusterMetadata.GetCurrentClusterName(),
		Clusters:          clusters,
	}, resp.ReplicationConfig)
	s.Equal(common.EmptyVersion, resp.GetFailoverVersion())
	s.Equal(isGlobalNamespace, resp.GetIsGlobalNamespace())
}

func (s *namespaceHandlerGlobalNamespaceEnabledMasterClusterSuite) TestRegisterGetNamespace_LocalNamespace_NoDefault() {
	namespace := s.getRandomNamespace()
	description := "some random description"
	email := "some random email"
	retention := int32(7)
	emitMetric := true
	activeClusterName := cluster.TestCurrentClusterName
	clusters := []*replicationpb.ClusterReplicationConfig{
		&replicationpb.ClusterReplicationConfig{
			ClusterName: activeClusterName,
		},
	}
	data := map[string]string{"some random key": "some random value"}
	isGlobalNamespace := false

	var expectedClusters []*replicationpb.ClusterReplicationConfig
	for _, name := range persistence.GetOrUseDefaultClusters(s.ClusterMetadata.GetCurrentClusterName(), nil) {
		expectedClusters = append(expectedClusters, &replicationpb.ClusterReplicationConfig{
			ClusterName: name,
		})
	}

	registerResp, err := s.handler.RegisterNamespace(context.Background(), &workflowservice.RegisterNamespaceRequest{
		Name:                                 namespace,
		Description:                          description,
		OwnerEmail:                           email,
		WorkflowExecutionRetentionPeriodDays: retention,
		EmitMetric:                           emitMetric,
		Clusters:                             clusters,
		ActiveClusterName:                    activeClusterName,
		Data:                                 data,
		IsGlobalNamespace:                    isGlobalNamespace,
	})
	s.NoError(err)
	s.Nil(registerResp)

	resp, err := s.handler.DescribeNamespace(context.Background(), &workflowservice.DescribeNamespaceRequest{
		Name: namespace,
	})
	s.NoError(err)

	s.NotEmpty(resp.NamespaceInfo.GetId())
	resp.NamespaceInfo.Id = ""
	s.Equal(&namespacepb.NamespaceInfo{
		Name:        namespace,
		State:       enumspb.NAMESPACE_STATE_REGISTERED,
		Description: description,
		OwnerEmail:  email,
		Data:        data,
		Id:          "",
	}, resp.NamespaceInfo)
	s.Equal(&namespacepb.NamespaceConfig{
		WorkflowExecutionRetentionPeriodInDays: retention,
		EmitMetric:                             &types.BoolValue{Value: emitMetric},
		HistoryArchivalState:                   enumspb.ARCHIVAL_STATE_DISABLED,
		HistoryArchivalUri:                     "",
		VisibilityArchivalState:                enumspb.ARCHIVAL_STATE_DISABLED,
		VisibilityArchivalUri:                  "",
		BadBinaries:                            &namespacepb.BadBinaries{Binaries: map[string]*namespacepb.BadBinaryInfo{}},
	}, resp.Config)
	s.Equal(&replicationpb.NamespaceReplicationConfig{
		ActiveClusterName: s.ClusterMetadata.GetCurrentClusterName(),
		Clusters:          expectedClusters,
	}, resp.ReplicationConfig)
	s.Equal(common.EmptyVersion, resp.GetFailoverVersion())
	s.Equal(isGlobalNamespace, resp.GetIsGlobalNamespace())
}

func (s *namespaceHandlerGlobalNamespaceEnabledMasterClusterSuite) TestUpdateGetNamespace_LocalNamespace_NoAttrSet() {
	namespace := s.getRandomNamespace()
	description := "some random description"
	email := "some random email"
	retention := int32(7)
	emitMetric := true
	data := map[string]string{"some random key": "some random value"}
	var clusters []*replicationpb.ClusterReplicationConfig
	for _, name := range persistence.GetOrUseDefaultClusters(s.ClusterMetadata.GetCurrentClusterName(), nil) {
		clusters = append(clusters, &replicationpb.ClusterReplicationConfig{
			ClusterName: name,
		})
	}
	isGlobalNamespace := false

	registerResp, err := s.handler.RegisterNamespace(context.Background(), &workflowservice.RegisterNamespaceRequest{
		Name:                                 namespace,
		Description:                          description,
		OwnerEmail:                           email,
		WorkflowExecutionRetentionPeriodDays: retention,
		EmitMetric:                           emitMetric,
		Clusters:                             clusters,
		ActiveClusterName:                    s.ClusterMetadata.GetCurrentClusterName(),
		Data:                                 data,
		IsGlobalNamespace:                    isGlobalNamespace,
	})
	s.NoError(err)
	s.Nil(registerResp)

	fnTest := func(info *namespacepb.NamespaceInfo, config *namespacepb.NamespaceConfig,
		replicationConfig *replicationpb.NamespaceReplicationConfig, isGlobalNamespace bool, failoverVersion int64) {
		s.NotEmpty(info.GetId())
		info.Id = ""
		s.Equal(&namespacepb.NamespaceInfo{
			Name:        namespace,
			State:       enumspb.NAMESPACE_STATE_REGISTERED,
			Description: description,
			OwnerEmail:  email,
			Data:        data,
			Id:          "",
		}, info)
		s.Equal(&namespacepb.NamespaceConfig{
			WorkflowExecutionRetentionPeriodInDays: retention,
			EmitMetric:                             &types.BoolValue{Value: emitMetric},
			HistoryArchivalState:                   enumspb.ARCHIVAL_STATE_DISABLED,
			HistoryArchivalUri:                     "",
			VisibilityArchivalState:                enumspb.ARCHIVAL_STATE_DISABLED,
			VisibilityArchivalUri:                  "",
			BadBinaries:                            &namespacepb.BadBinaries{Binaries: map[string]*namespacepb.BadBinaryInfo{}},
		}, config)
		s.Equal(&replicationpb.NamespaceReplicationConfig{
			ActiveClusterName: s.ClusterMetadata.GetCurrentClusterName(),
			Clusters:          clusters,
		}, replicationConfig)
		s.Equal(common.EmptyVersion, failoverVersion)
		s.Equal(isGlobalNamespace, isGlobalNamespace)
	}

	updateResp, err := s.handler.UpdateNamespace(context.Background(), &workflowservice.UpdateNamespaceRequest{
		Name: namespace,
	})
	s.NoError(err)
	fnTest(
		updateResp.NamespaceInfo,
		updateResp.Config,
		updateResp.ReplicationConfig,
		updateResp.GetIsGlobalNamespace(),
		updateResp.GetFailoverVersion(),
	)

	getResp, err := s.handler.DescribeNamespace(context.Background(), &workflowservice.DescribeNamespaceRequest{
		Name: namespace,
	})
	s.NoError(err)
	fnTest(
		getResp.NamespaceInfo,
		getResp.Config,
		getResp.ReplicationConfig,
		getResp.GetIsGlobalNamespace(),
		getResp.GetFailoverVersion(),
	)
}

func (s *namespaceHandlerGlobalNamespaceEnabledMasterClusterSuite) TestUpdateGetNamespace_LocalNamespace_AllAttrSet() {
	namespace := s.getRandomNamespace()
	isGlobalNamespace := false
	registerResp, err := s.handler.RegisterNamespace(context.Background(), &workflowservice.RegisterNamespaceRequest{
		Name:                                 namespace,
		IsGlobalNamespace:                    isGlobalNamespace,
		WorkflowExecutionRetentionPeriodDays: 1,
	})
	s.NoError(err)
	s.Nil(registerResp)

	description := "some random description"
	email := "some random email"
	retention := int32(7)
	emitMetric := true
	data := map[string]string{"some random key": "some random value"}
	var clusters []*replicationpb.ClusterReplicationConfig
	for _, name := range persistence.GetOrUseDefaultClusters(s.ClusterMetadata.GetCurrentClusterName(), nil) {
		clusters = append(clusters, &replicationpb.ClusterReplicationConfig{
			ClusterName: name,
		})
	}

	fnTest := func(info *namespacepb.NamespaceInfo, config *namespacepb.NamespaceConfig,
		replicationConfig *replicationpb.NamespaceReplicationConfig, isGlobalNamespace bool, failoverVersion int64) {
		s.NotEmpty(info.GetId())
		info.Id = ""
		s.Equal(&namespacepb.NamespaceInfo{
			Name:        namespace,
			State:       enumspb.NAMESPACE_STATE_REGISTERED,
			Description: description,
			OwnerEmail:  email,
			Data:        data,
			Id:          "",
		}, info)
		s.Equal(&namespacepb.NamespaceConfig{
			WorkflowExecutionRetentionPeriodInDays: retention,
			EmitMetric:                             &types.BoolValue{Value: emitMetric},
			HistoryArchivalState:                   enumspb.ARCHIVAL_STATE_DISABLED,
			HistoryArchivalUri:                     "",
			VisibilityArchivalState:                enumspb.ARCHIVAL_STATE_DISABLED,
			VisibilityArchivalUri:                  "",
			BadBinaries:                            &namespacepb.BadBinaries{Binaries: map[string]*namespacepb.BadBinaryInfo{}},
		}, config)
		s.Equal(&replicationpb.NamespaceReplicationConfig{
			ActiveClusterName: s.ClusterMetadata.GetCurrentClusterName(),
			Clusters:          clusters,
		}, replicationConfig)
		s.Equal(common.EmptyVersion, failoverVersion)
		s.Equal(isGlobalNamespace, isGlobalNamespace)
	}

	updateResp, err := s.handler.UpdateNamespace(context.Background(), &workflowservice.UpdateNamespaceRequest{
		Name: namespace,
		UpdateInfo: &namespacepb.UpdateNamespaceInfo{
			Description: description,
			OwnerEmail:  email,
			Data:        data,
		},
		Config: &namespacepb.NamespaceConfig{
			WorkflowExecutionRetentionPeriodInDays: retention,
			EmitMetric:                             &types.BoolValue{Value: emitMetric},
			HistoryArchivalState:                   enumspb.ARCHIVAL_STATE_DISABLED,
			HistoryArchivalUri:                     "",
			VisibilityArchivalState:                enumspb.ARCHIVAL_STATE_DISABLED,
			VisibilityArchivalUri:                  "",
			BadBinaries:                            &namespacepb.BadBinaries{Binaries: map[string]*namespacepb.BadBinaryInfo{}},
		},
		ReplicationConfig: &replicationpb.NamespaceReplicationConfig{
			ActiveClusterName: s.ClusterMetadata.GetCurrentClusterName(),
			Clusters:          clusters,
		},
	})
	s.NoError(err)
	fnTest(
		updateResp.NamespaceInfo,
		updateResp.Config,
		updateResp.ReplicationConfig,
		updateResp.GetIsGlobalNamespace(),
		updateResp.GetFailoverVersion(),
	)

	getResp, err := s.handler.DescribeNamespace(context.Background(), &workflowservice.DescribeNamespaceRequest{
		Name: namespace,
	})
	s.NoError(err)
	fnTest(
		getResp.NamespaceInfo,
		getResp.Config,
		getResp.ReplicationConfig,
		getResp.GetIsGlobalNamespace(),
		getResp.GetFailoverVersion(),
	)
}

func (s *namespaceHandlerGlobalNamespaceEnabledMasterClusterSuite) TestRegisterGetNamespace_GlobalNamespace_AllDefault() {
	namespace := s.getRandomNamespace()
	isGlobalNamespace := true
	var clusters []*replicationpb.ClusterReplicationConfig
	for _, name := range persistence.GetOrUseDefaultClusters(s.ClusterMetadata.GetCurrentClusterName(), nil) {
		clusters = append(clusters, &replicationpb.ClusterReplicationConfig{
			ClusterName: name,
		})
	}

	s.mockProducer.On("Publish", mock.Anything).Return(nil).Once()

	retention := int32(1)
	registerResp, err := s.handler.RegisterNamespace(context.Background(), &workflowservice.RegisterNamespaceRequest{
		Name:                                 namespace,
		IsGlobalNamespace:                    isGlobalNamespace,
		WorkflowExecutionRetentionPeriodDays: retention,
	})
	s.NoError(err)
	s.Nil(registerResp)

	resp, err := s.handler.DescribeNamespace(context.Background(), &workflowservice.DescribeNamespaceRequest{
		Name: namespace,
	})
	s.NoError(err)

	s.NotEmpty(resp.NamespaceInfo.GetId())
	resp.NamespaceInfo.Id = ""
	s.Equal(&namespacepb.NamespaceInfo{
		Name:        namespace,
		State:       enumspb.NAMESPACE_STATE_REGISTERED,
		Description: "",
		OwnerEmail:  "",
		Data:        map[string]string{},
		Id:          "",
	}, resp.NamespaceInfo)
	s.Equal(&namespacepb.NamespaceConfig{
		WorkflowExecutionRetentionPeriodInDays: retention,
		EmitMetric:                             &types.BoolValue{Value: false},
		HistoryArchivalState:                   enumspb.ARCHIVAL_STATE_DISABLED,
		HistoryArchivalUri:                     "",
		VisibilityArchivalState:                enumspb.ARCHIVAL_STATE_DISABLED,
		VisibilityArchivalUri:                  "",
		BadBinaries:                            &namespacepb.BadBinaries{Binaries: map[string]*namespacepb.BadBinaryInfo{}},
	}, resp.Config)
	s.Equal(&replicationpb.NamespaceReplicationConfig{
		ActiveClusterName: s.ClusterMetadata.GetCurrentClusterName(),
		Clusters:          clusters,
	}, resp.ReplicationConfig)
	s.Equal(s.ClusterMetadata.GetNextFailoverVersion(s.ClusterMetadata.GetCurrentClusterName(), 0), resp.GetFailoverVersion())
	s.Equal(isGlobalNamespace, resp.GetIsGlobalNamespace())
}

func (s *namespaceHandlerGlobalNamespaceEnabledMasterClusterSuite) TestRegisterGetNamespace_GlobalNamespace_NoDefault() {
	namespace := s.getRandomNamespace()
	description := "some random description"
	email := "some random email"
	retention := int32(7)
	emitMetric := true
	activeClusterName := ""
	clusters := []*replicationpb.ClusterReplicationConfig{}
	for clusterName := range s.ClusterMetadata.GetAllClusterInfo() {
		if clusterName != s.ClusterMetadata.GetCurrentClusterName() {
			activeClusterName = clusterName
		}
		clusters = append(clusters, &replicationpb.ClusterReplicationConfig{
			ClusterName: clusterName,
		})
	}
	s.True(len(activeClusterName) > 0)
	s.True(len(clusters) > 1)
	data := map[string]string{"some random key": "some random value"}
	isGlobalNamespace := true

	s.mockProducer.On("Publish", mock.Anything).Return(nil).Once()

	registerResp, err := s.handler.RegisterNamespace(context.Background(), &workflowservice.RegisterNamespaceRequest{
		Name:                                 namespace,
		Description:                          description,
		OwnerEmail:                           email,
		WorkflowExecutionRetentionPeriodDays: retention,
		EmitMetric:                           emitMetric,
		Clusters:                             clusters,
		ActiveClusterName:                    activeClusterName,
		Data:                                 data,
		IsGlobalNamespace:                    isGlobalNamespace,
	})
	s.NoError(err)
	s.Nil(registerResp)

	resp, err := s.handler.DescribeNamespace(context.Background(), &workflowservice.DescribeNamespaceRequest{
		Name: namespace,
	})
	s.NoError(err)

	s.NotEmpty(resp.NamespaceInfo.GetId())
	resp.NamespaceInfo.Id = ""
	s.Equal(&namespacepb.NamespaceInfo{
		Name:        namespace,
		State:       enumspb.NAMESPACE_STATE_REGISTERED,
		Description: description,
		OwnerEmail:  email,
		Data:        data,
		Id:          "",
	}, resp.NamespaceInfo)
	s.Equal(&namespacepb.NamespaceConfig{
		WorkflowExecutionRetentionPeriodInDays: retention,
		EmitMetric:                             &types.BoolValue{Value: emitMetric},
		HistoryArchivalState:                   enumspb.ARCHIVAL_STATE_DISABLED,
		HistoryArchivalUri:                     "",
		VisibilityArchivalState:                enumspb.ARCHIVAL_STATE_DISABLED,
		VisibilityArchivalUri:                  "",
		BadBinaries:                            &namespacepb.BadBinaries{Binaries: map[string]*namespacepb.BadBinaryInfo{}},
	}, resp.Config)
	s.Equal(&replicationpb.NamespaceReplicationConfig{
		ActiveClusterName: activeClusterName,
		Clusters:          clusters,
	}, resp.ReplicationConfig)
	s.Equal(s.ClusterMetadata.GetNextFailoverVersion(activeClusterName, 0), resp.GetFailoverVersion())
	s.Equal(isGlobalNamespace, resp.GetIsGlobalNamespace())
}

func (s *namespaceHandlerGlobalNamespaceEnabledMasterClusterSuite) TestUpdateGetNamespace_GlobalNamespace_NoAttrSet() {
	namespace := s.getRandomNamespace()
	description := "some random description"
	email := "some random email"
	retention := int32(7)
	emitMetric := true
	data := map[string]string{"some random key": "some random value"}
	activeClusterName := ""
	clusters := []*replicationpb.ClusterReplicationConfig{}
	for clusterName := range s.ClusterMetadata.GetAllClusterInfo() {
		if clusterName != s.ClusterMetadata.GetCurrentClusterName() {
			activeClusterName = clusterName
		}
		clusters = append(clusters, &replicationpb.ClusterReplicationConfig{
			ClusterName: clusterName,
		})
	}
	s.True(len(activeClusterName) > 0)
	s.True(len(clusters) > 1)
	isGlobalNamespace := true

	s.mockProducer.On("Publish", mock.Anything).Return(nil).Twice()

	registerResp, err := s.handler.RegisterNamespace(context.Background(), &workflowservice.RegisterNamespaceRequest{
		Name:                                 namespace,
		Description:                          description,
		OwnerEmail:                           email,
		WorkflowExecutionRetentionPeriodDays: retention,
		EmitMetric:                           emitMetric,
		Clusters:                             clusters,
		ActiveClusterName:                    activeClusterName,
		Data:                                 data,
		IsGlobalNamespace:                    isGlobalNamespace,
	})
	s.NoError(err)
	s.Nil(registerResp)

	fnTest := func(info *namespacepb.NamespaceInfo, config *namespacepb.NamespaceConfig,
		replicationConfig *replicationpb.NamespaceReplicationConfig, isGlobalNamespace bool, failoverVersion int64) {
		s.NotEmpty(info.GetId())
		info.Id = ""
		s.Equal(&namespacepb.NamespaceInfo{
			Name:        namespace,
			State:       enumspb.NAMESPACE_STATE_REGISTERED,
			Description: description,
			OwnerEmail:  email,
			Data:        data,
			Id:          "",
		}, info)
		s.Equal(&namespacepb.NamespaceConfig{
			WorkflowExecutionRetentionPeriodInDays: retention,
			EmitMetric:                             &types.BoolValue{Value: emitMetric},
			HistoryArchivalState:                   enumspb.ARCHIVAL_STATE_DISABLED,
			HistoryArchivalUri:                     "",
			VisibilityArchivalState:                enumspb.ARCHIVAL_STATE_DISABLED,
			VisibilityArchivalUri:                  "",
			BadBinaries:                            &namespacepb.BadBinaries{Binaries: map[string]*namespacepb.BadBinaryInfo{}},
		}, config)
		s.Equal(&replicationpb.NamespaceReplicationConfig{
			ActiveClusterName: activeClusterName,
			Clusters:          clusters,
		}, replicationConfig)
		s.Equal(s.ClusterMetadata.GetNextFailoverVersion(activeClusterName, 0), failoverVersion)
		s.Equal(isGlobalNamespace, isGlobalNamespace)
	}

	updateResp, err := s.handler.UpdateNamespace(context.Background(), &workflowservice.UpdateNamespaceRequest{
		Name: namespace,
	})
	s.NoError(err)
	fnTest(
		updateResp.NamespaceInfo,
		updateResp.Config,
		updateResp.ReplicationConfig,
		updateResp.GetIsGlobalNamespace(),
		updateResp.GetFailoverVersion(),
	)

	getResp, err := s.handler.DescribeNamespace(context.Background(), &workflowservice.DescribeNamespaceRequest{
		Name: namespace,
	})
	s.NoError(err)
	fnTest(
		getResp.NamespaceInfo,
		getResp.Config,
		getResp.ReplicationConfig,
		getResp.GetIsGlobalNamespace(),
		getResp.GetFailoverVersion(),
	)
}

func (s *namespaceHandlerGlobalNamespaceEnabledMasterClusterSuite) TestUpdateGetNamespace_GlobalNamespace_AllAttrSet() {
	namespace := s.getRandomNamespace()
	activeClusterName := ""
	clusters := []*replicationpb.ClusterReplicationConfig{}
	for clusterName := range s.ClusterMetadata.GetAllClusterInfo() {
		if clusterName != s.ClusterMetadata.GetCurrentClusterName() {
			activeClusterName = clusterName
		}
		clusters = append(clusters, &replicationpb.ClusterReplicationConfig{
			ClusterName: clusterName,
		})
	}
	s.True(len(activeClusterName) > 0)
	s.True(len(clusters) > 1)
	isGlobalNamespace := true

	s.mockProducer.On("Publish", mock.Anything).Return(nil).Twice()

	registerResp, err := s.handler.RegisterNamespace(context.Background(), &workflowservice.RegisterNamespaceRequest{
		Name:                                 namespace,
		IsGlobalNamespace:                    isGlobalNamespace,
		Clusters:                             clusters,
		ActiveClusterName:                    activeClusterName,
		WorkflowExecutionRetentionPeriodDays: 1,
	})
	s.NoError(err)
	s.Nil(registerResp)

	description := "some random description"
	email := "some random email"
	retention := int32(7)
	emitMetric := true
	data := map[string]string{"some random key": "some random value"}

	fnTest := func(info *namespacepb.NamespaceInfo, config *namespacepb.NamespaceConfig,
		replicationConfig *replicationpb.NamespaceReplicationConfig, isGlobalNamespace bool, failoverVersion int64) {
		s.NotEmpty(info.GetId())
		info.Id = ""
		s.Equal(&namespacepb.NamespaceInfo{
			Name:        namespace,
			State:       enumspb.NAMESPACE_STATE_REGISTERED,
			Description: description,
			OwnerEmail:  email,
			Data:        data,
			Id:          "",
		}, info)
		s.Equal(&namespacepb.NamespaceConfig{
			WorkflowExecutionRetentionPeriodInDays: retention,
			EmitMetric:                             &types.BoolValue{Value: emitMetric},
			HistoryArchivalState:                   enumspb.ARCHIVAL_STATE_DISABLED,
			HistoryArchivalUri:                     "",
			VisibilityArchivalState:                enumspb.ARCHIVAL_STATE_DISABLED,
			VisibilityArchivalUri:                  "",
			BadBinaries:                            &namespacepb.BadBinaries{Binaries: map[string]*namespacepb.BadBinaryInfo{}},
		}, config)
		s.Equal(&replicationpb.NamespaceReplicationConfig{
			ActiveClusterName: activeClusterName,
			Clusters:          clusters,
		}, replicationConfig)
		s.Equal(s.ClusterMetadata.GetNextFailoverVersion(activeClusterName, 0), failoverVersion)
		s.Equal(isGlobalNamespace, isGlobalNamespace)
	}

	updateResp, err := s.handler.UpdateNamespace(context.Background(), &workflowservice.UpdateNamespaceRequest{
		Name: namespace,
		UpdateInfo: &namespacepb.UpdateNamespaceInfo{
			Description: description,
			OwnerEmail:  email,
			Data:        data,
		},
		Config: &namespacepb.NamespaceConfig{
			WorkflowExecutionRetentionPeriodInDays: retention,
			EmitMetric:                             &types.BoolValue{Value: emitMetric},
			HistoryArchivalState:                   enumspb.ARCHIVAL_STATE_DISABLED,
			HistoryArchivalUri:                     "",
			VisibilityArchivalState:                enumspb.ARCHIVAL_STATE_DISABLED,
			VisibilityArchivalUri:                  "",
			BadBinaries:                            &namespacepb.BadBinaries{Binaries: map[string]*namespacepb.BadBinaryInfo{}},
		},
		ReplicationConfig: &replicationpb.NamespaceReplicationConfig{
			ActiveClusterName: "",
			Clusters:          clusters,
		},
	})
	s.NoError(err)
	fnTest(
		updateResp.NamespaceInfo,
		updateResp.Config,
		updateResp.ReplicationConfig,
		updateResp.GetIsGlobalNamespace(),
		updateResp.GetFailoverVersion(),
	)

	getResp, err := s.handler.DescribeNamespace(context.Background(), &workflowservice.DescribeNamespaceRequest{
		Name: namespace,
	})
	s.NoError(err)
	fnTest(
		getResp.NamespaceInfo,
		getResp.Config,
		getResp.ReplicationConfig,
		getResp.GetIsGlobalNamespace(),
		getResp.GetFailoverVersion(),
	)
}

func (s *namespaceHandlerGlobalNamespaceEnabledMasterClusterSuite) TestUpdateGetNamespace_GlobalNamespace_Failover() {
	namespace := s.getRandomNamespace()
	description := "some random description"
	email := "some random email"
	retention := int32(7)
	emitMetric := true
	data := map[string]string{"some random key": "some random value"}
	prevActiveClusterName := ""
	nextActiveClusterName := s.ClusterMetadata.GetCurrentClusterName()
	clusters := []*replicationpb.ClusterReplicationConfig{}
	for clusterName := range s.ClusterMetadata.GetAllClusterInfo() {
		if clusterName != nextActiveClusterName {
			prevActiveClusterName = clusterName
		}
		clusters = append(clusters, &replicationpb.ClusterReplicationConfig{
			ClusterName: clusterName,
		})
	}
	s.True(len(prevActiveClusterName) > 0)
	s.True(len(clusters) > 1)
	isGlobalNamespace := true

	s.mockProducer.On("Publish", mock.Anything).Return(nil).Twice()

	registerResp, err := s.handler.RegisterNamespace(context.Background(), &workflowservice.RegisterNamespaceRequest{
		Name:                                 namespace,
		Description:                          description,
		OwnerEmail:                           email,
		WorkflowExecutionRetentionPeriodDays: retention,
		EmitMetric:                           emitMetric,
		Clusters:                             clusters,
		ActiveClusterName:                    prevActiveClusterName,
		Data:                                 data,
		IsGlobalNamespace:                    isGlobalNamespace,
	})
	s.NoError(err)
	s.Nil(registerResp)

	fnTest := func(info *namespacepb.NamespaceInfo, config *namespacepb.NamespaceConfig,
		replicationConfig *replicationpb.NamespaceReplicationConfig, isGlobalNamespace bool, failoverVersion int64) {
		s.NotEmpty(info.GetId())
		info.Id = ""
		s.Equal(&namespacepb.NamespaceInfo{
			Name:        namespace,
			State:       enumspb.NAMESPACE_STATE_REGISTERED,
			Description: description,
			OwnerEmail:  email,
			Data:        data,
			Id:          "",
		}, info)
		s.Equal(&namespacepb.NamespaceConfig{
			WorkflowExecutionRetentionPeriodInDays: retention,
			EmitMetric:                             &types.BoolValue{Value: emitMetric},
			HistoryArchivalState:                   enumspb.ARCHIVAL_STATE_DISABLED,
			HistoryArchivalUri:                     "",
			VisibilityArchivalState:                enumspb.ARCHIVAL_STATE_DISABLED,
			VisibilityArchivalUri:                  "",
			BadBinaries:                            &namespacepb.BadBinaries{Binaries: map[string]*namespacepb.BadBinaryInfo{}},
		}, config)
		s.Equal(&replicationpb.NamespaceReplicationConfig{
			ActiveClusterName: nextActiveClusterName,
			Clusters:          clusters,
		}, replicationConfig)
		s.Equal(s.ClusterMetadata.GetNextFailoverVersion(
			nextActiveClusterName,
			s.ClusterMetadata.GetNextFailoverVersion(prevActiveClusterName, 0),
		), failoverVersion)
		s.Equal(isGlobalNamespace, isGlobalNamespace)
	}

	updateResp, err := s.handler.UpdateNamespace(context.Background(), &workflowservice.UpdateNamespaceRequest{
		Name: namespace,
		ReplicationConfig: &replicationpb.NamespaceReplicationConfig{
			ActiveClusterName: s.ClusterMetadata.GetCurrentClusterName(),
		},
	})
	s.NoError(err)
	fnTest(
		updateResp.NamespaceInfo,
		updateResp.Config,
		updateResp.ReplicationConfig,
		updateResp.GetIsGlobalNamespace(),
		updateResp.GetFailoverVersion(),
	)

	getResp, err := s.handler.DescribeNamespace(context.Background(), &workflowservice.DescribeNamespaceRequest{
		Name: namespace,
	})
	s.NoError(err)
	fnTest(
		getResp.NamespaceInfo,
		getResp.Config,
		getResp.ReplicationConfig,
		getResp.GetIsGlobalNamespace(),
		getResp.GetFailoverVersion(),
	)
}

func (s *namespaceHandlerGlobalNamespaceEnabledMasterClusterSuite) getRandomNamespace() string {
	return "namespace" + uuid.New()
}
