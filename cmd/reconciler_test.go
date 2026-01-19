package main

import (
	"testing"

	sourceController "github.com/fluxcd/source-controller/api/v1"
	"go.uber.org/zap/zaptest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

// MockAnnotator tracks calls to annotation methods
type MockAnnotator struct {
	AnnotatedGitRepos []sourceController.GitRepository
	AnnotatedOciRepos []sourceController.OCIRepository
	GitRepoErrors     map[string]error
	OciRepoErrors     map[string]error
}

func NewMockAnnotator() *MockAnnotator {
	return &MockAnnotator{
		AnnotatedGitRepos: make([]sourceController.GitRepository, 0),
		AnnotatedOciRepos: make([]sourceController.OCIRepository, 0),
		GitRepoErrors:     make(map[string]error),
		OciRepoErrors:     make(map[string]error),
	}
}

func (m *MockAnnotator) AnnotateGitRepository(repository sourceController.GitRepository) error {
	if err, ok := m.GitRepoErrors[repository.Name]; ok {
		return err
	}
	m.AnnotatedGitRepos = append(m.AnnotatedGitRepos, repository)
	return nil
}

func (m *MockAnnotator) AnnotateOciRepository(repository sourceController.OCIRepository) error {
	if err, ok := m.OciRepoErrors[repository.Name]; ok {
		return err
	}
	m.AnnotatedOciRepos = append(m.AnnotatedOciRepos, repository)
	return nil
}

func (m *MockAnnotator) GetAnnotatedGitRepositories() []sourceController.GitRepository {
	return m.AnnotatedGitRepos
}

func (m *MockAnnotator) GetAnnotatedOciRepositories() []sourceController.OCIRepository {
	return m.AnnotatedOciRepos
}

func (m *MockAnnotator) Reset() {
	m.AnnotatedGitRepos = make([]sourceController.GitRepository, 0)
	m.AnnotatedOciRepos = make([]sourceController.OCIRepository, 0)
	m.GitRepoErrors = make(map[string]error)
	m.OciRepoErrors = make(map[string]error)
}

// createMockClient creates a mock dynamic client that returns predefined test data
func createMockClient(gitRepos []sourceController.GitRepository, ociRepos []sourceController.OCIRepository) dynamic.Interface {
	scheme := runtime.NewScheme()
	sourceController.AddToScheme(scheme)

	// Convert slices to individual objects for the fake client
	objects := make([]runtime.Object, 0, len(gitRepos)+len(ociRepos))

	// Add individual GitRepository objects
	for i := range gitRepos {
		objects = append(objects, &gitRepos[i])
	}

	// Add individual OCIRepository objects
	for i := range ociRepos {
		objects = append(objects, &ociRepos[i])
	}

	return dynamicfake.NewSimpleDynamicClient(scheme, objects...)
}

// Helper functions to create test repository objects
func createTestGitRepository(name, namespace, url, branch string) sourceController.GitRepository {
	repo := sourceController.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: sourceController.GitRepositorySpec{
			URL: url,
		},
	}
	if branch != "" {
		repo.Spec.Reference = &sourceController.GitRepositoryRef{
			Branch: branch,
		}
	}
	return repo
}

func createTestOCIRepository(name, namespace, url, tag string) sourceController.OCIRepository {
	repo := sourceController.OCIRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: sourceController.OCIRepositorySpec{
			URL: url,
		},
	}
	if tag != "" {
		repo.Spec.Reference = &sourceController.OCIRepositoryRef{
			Tag: tag,
		}
	}
	return repo
}

func TestReconcileGitRepositories(t *testing.T) {
	tests := []struct {
		name           string
		repoURL        string
		ref            string
		gitRepos       []sourceController.GitRepository
		expectedCalls  int
		expectedNames  []string
		expectedURLs   []string
		expectedBranch string
	}{
		{
			name:    "matching HTTPS URL",
			repoURL: "https://github.com/owner/repo",
			ref:     "refs/heads/main",
			gitRepos: []sourceController.GitRepository{
				createTestGitRepository("repo1", "default", "https://github.com/owner/repo", ""),
			},
			expectedCalls: 1,
			expectedNames: []string{"repo1"},
			expectedURLs:  []string{"https://github.com/owner/repo"},
		},
		{
			name:    "matching SSH URL (git@ format)",
			repoURL: "git@github.com:owner/repo",
			ref:     "refs/heads/main",
			gitRepos: []sourceController.GitRepository{
				createTestGitRepository("repo2", "default", "https://github.com/owner/repo", ""),
			},
			expectedCalls: 1,
			expectedNames: []string{"repo2"},
			expectedURLs:  []string{"https://github.com/owner/repo"},
		},
		{
			name:    "matching SSH URL (ssh:// format)",
			repoURL: "ssh://git@github.com/owner/repo",
			ref:     "refs/heads/main",
			gitRepos: []sourceController.GitRepository{
				createTestGitRepository("repo3", "default", "https://github.com/owner/repo", ""),
			},
			expectedCalls: 1,
			expectedNames: []string{"repo3"},
			expectedURLs:  []string{"https://github.com/owner/repo"},
		},
		{
			name:    "matching URL with .git suffix",
			repoURL: "https://github.com/owner/repo.git",
			ref:     "refs/heads/main",
			gitRepos: []sourceController.GitRepository{
				createTestGitRepository("repo4", "default", "https://github.com/owner/repo", ""),
			},
			expectedCalls: 1,
			expectedNames: []string{"repo4"},
			expectedURLs:  []string{"https://github.com/owner/repo"},
		},
		{
			name:    "non-matching URL",
			repoURL: "https://github.com/owner/repo",
			ref:     "refs/heads/main",
			gitRepos: []sourceController.GitRepository{
				createTestGitRepository("repo5", "default", "https://github.com/owner/other", ""),
			},
			expectedCalls: 0,
			expectedNames: []string{},
			expectedURLs:  []string{},
		},
		{
			name:    "matching URL with branch filter - branch matches",
			repoURL: "https://github.com/owner/repo",
			ref:     "refs/heads/main",
			gitRepos: []sourceController.GitRepository{
				createTestGitRepository("repo6", "default", "https://github.com/owner/repo", "main"),
			},
			expectedCalls: 1,
			expectedNames: []string{"repo6"},
			expectedURLs:  []string{"https://github.com/owner/repo"},
		},
		{
			name:    "matching URL with branch filter - branch doesn't match",
			repoURL: "https://github.com/owner/repo",
			ref:     "refs/heads/main",
			gitRepos: []sourceController.GitRepository{
				createTestGitRepository("repo7", "default", "https://github.com/owner/repo", "develop"),
			},
			expectedCalls: 0,
			expectedNames: []string{},
			expectedURLs:  []string{},
		},
		{
			name:    "matching URL with branch filter - no branch in ref",
			repoURL: "https://github.com/owner/repo",
			ref:     "refs/tags/v1.0.0",
			gitRepos: []sourceController.GitRepository{
				createTestGitRepository("repo8", "default", "https://github.com/owner/repo", "main"),
			},
			expectedCalls: 1,
			expectedNames: []string{"repo8"},
			expectedURLs:  []string{"https://github.com/owner/repo"},
		},
		{
			name:    "multiple matching repositories",
			repoURL: "https://github.com/owner/repo",
			ref:     "refs/heads/main",
			gitRepos: []sourceController.GitRepository{
				createTestGitRepository("repo9", "default", "https://github.com/owner/repo", ""),
				createTestGitRepository("repo10", "kube-system", "https://github.com/owner/repo", ""),
			},
			expectedCalls: 2,
			expectedNames: []string{"repo9", "repo10"},
			expectedURLs:  []string{"https://github.com/owner/repo", "https://github.com/owner/repo"},
		},
		{
			name:    "case insensitive URL matching",
			repoURL: "https://github.com/Owner/Repo",
			ref:     "refs/heads/main",
			gitRepos: []sourceController.GitRepository{
				createTestGitRepository("repo11", "default", "https://github.com/owner/repo", ""),
			},
			expectedCalls: 1,
			expectedNames: []string{"repo11"},
			expectedURLs:  []string{"https://github.com/owner/repo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAnnotator := NewMockAnnotator()
			mockClient := createMockClient(tt.gitRepos, []sourceController.OCIRepository{})
			logger := zaptest.NewLogger(t)

			reconciler := NewReconcilerWithAnnotator(mockClient, mockAnnotator, logger)

			reconciler.ReconcileGitRepositories(tt.repoURL, tt.ref)

			annotatedRepos := mockAnnotator.GetAnnotatedGitRepositories()
			if len(annotatedRepos) != tt.expectedCalls {
				t.Errorf("expected %d annotation calls, got %d", tt.expectedCalls, len(annotatedRepos))
			}

			if len(annotatedRepos) != len(tt.expectedNames) {
				t.Errorf("expected %d annotated repos, got %d", len(tt.expectedNames), len(annotatedRepos))
			}

			for i, expectedName := range tt.expectedNames {
				if i >= len(annotatedRepos) {
					break
				}
				if annotatedRepos[i].Name != expectedName {
					t.Errorf("annotation call %d: expected name %s, got %s", i, expectedName, annotatedRepos[i].Name)
				}
				if i < len(tt.expectedURLs) && annotatedRepos[i].Spec.URL != tt.expectedURLs[i] {
					t.Errorf("annotation call %d: expected URL %s, got %s", i, tt.expectedURLs[i], annotatedRepos[i].Spec.URL)
				}
			}
		})
	}
}

func TestReconcileOciSources(t *testing.T) {
	tests := []struct {
		name          string
		ociUrl        string
		tag           string
		ociRepos      []sourceController.OCIRepository
		expectedCalls int
		expectedNames []string
		expectedURLs  []string
		expectedTags  []string
	}{
		{
			name:   "matching URL and tag",
			ociUrl: "oci://registry.example.com/namespace/image",
			tag:    "v1.0.0",
			ociRepos: []sourceController.OCIRepository{
				createTestOCIRepository("oci1", "default", "oci://registry.example.com/namespace/image", "v1.0.0"),
			},
			expectedCalls: 1,
			expectedNames: []string{"oci1"},
			expectedURLs:  []string{"oci://registry.example.com/namespace/image"},
			expectedTags:  []string{"v1.0.0"},
		},
		{
			name:   "non-matching URL",
			ociUrl: "oci://registry.example.com/namespace/image",
			tag:    "v1.0.0",
			ociRepos: []sourceController.OCIRepository{
				createTestOCIRepository("oci2", "default", "oci://registry.example.com/namespace/other", "v1.0.0"),
			},
			expectedCalls: 0,
			expectedNames: []string{},
			expectedURLs:  []string{},
			expectedTags:  []string{},
		},
		{
			name:   "non-matching tag",
			ociUrl: "oci://registry.example.com/namespace/image",
			tag:    "v1.0.0",
			ociRepos: []sourceController.OCIRepository{
				createTestOCIRepository("oci3", "default", "oci://registry.example.com/namespace/image", "v2.0.0"),
			},
			expectedCalls: 0,
			expectedNames: []string{},
			expectedURLs:  []string{},
			expectedTags:  []string{},
		},
		{
			name:   "multiple matching repositories",
			ociUrl: "oci://registry.example.com/namespace/image",
			tag:    "v1.0.0",
			ociRepos: []sourceController.OCIRepository{
				createTestOCIRepository("oci5", "default", "oci://registry.example.com/namespace/image", "v1.0.0"),
				createTestOCIRepository("oci6", "kube-system", "oci://registry.example.com/namespace/image", "v1.0.0"),
			},
			expectedCalls: 2,
			expectedNames: []string{"oci5", "oci6"},
			expectedURLs:  []string{"oci://registry.example.com/namespace/image", "oci://registry.example.com/namespace/image"},
			expectedTags:  []string{"v1.0.0", "v1.0.0"},
		},
		{
			name:   "different URLs with same tag",
			ociUrl: "oci://registry.example.com/namespace/image",
			tag:    "v1.0.0",
			ociRepos: []sourceController.OCIRepository{
				createTestOCIRepository("oci7", "default", "oci://registry.example.com/namespace/image", "v1.0.0"),
				createTestOCIRepository("oci8", "default", "oci://registry.example.com/namespace/other", "v1.0.0"),
			},
			expectedCalls: 1,
			expectedNames: []string{"oci7"},
			expectedURLs:  []string{"oci://registry.example.com/namespace/image"},
			expectedTags:  []string{"v1.0.0"},
		},
		{
			name:   "same URL with different tags",
			ociUrl: "oci://registry.example.com/namespace/image",
			tag:    "v1.0.0",
			ociRepos: []sourceController.OCIRepository{
				createTestOCIRepository("oci9", "default", "oci://registry.example.com/namespace/image", "v1.0.0"),
				createTestOCIRepository("oci10", "default", "oci://registry.example.com/namespace/image", "v2.0.0"),
			},
			expectedCalls: 1,
			expectedNames: []string{"oci9"},
			expectedURLs:  []string{"oci://registry.example.com/namespace/image"},
			expectedTags:  []string{"v1.0.0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAnnotator := NewMockAnnotator()
			mockClient := createMockClient([]sourceController.GitRepository{}, tt.ociRepos)
			logger := zaptest.NewLogger(t)

			reconciler := NewReconcilerWithAnnotator(mockClient, mockAnnotator, logger)

			reconciler.ReconcileOciSources(tt.ociUrl, tt.tag)

			annotatedRepos := mockAnnotator.GetAnnotatedOciRepositories()
			if len(annotatedRepos) != tt.expectedCalls {
				t.Errorf("expected %d annotation calls, got %d", tt.expectedCalls, len(annotatedRepos))
			}

			if len(annotatedRepos) != len(tt.expectedNames) {
				t.Errorf("expected %d annotated repos, got %d", len(tt.expectedNames), len(annotatedRepos))
			}

			for i, expectedName := range tt.expectedNames {
				if i >= len(annotatedRepos) {
					break
				}
				if annotatedRepos[i].Name != expectedName {
					t.Errorf("annotation call %d: expected name %s, got %s", i, expectedName, annotatedRepos[i].Name)
				}
				if i < len(tt.expectedURLs) && annotatedRepos[i].Spec.URL != tt.expectedURLs[i] {
					t.Errorf("annotation call %d: expected URL %s, got %s", i, tt.expectedURLs[i], annotatedRepos[i].Spec.URL)
				}
				if i < len(tt.expectedTags) {
					actualTag := ""
					if annotatedRepos[i].Spec.Reference != nil {
						actualTag = annotatedRepos[i].Spec.Reference.Tag
					}
					if actualTag != tt.expectedTags[i] {
						t.Errorf("annotation call %d: expected tag %s, got %s", i, tt.expectedTags[i], actualTag)
					}
				}
			}
		})
	}
}
