import { auth } from '$stores/auth';
import { get } from 'svelte/store';

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080';

async function getToken(): Promise<string | null> {
	return get(auth).token;
}

async function fetchAPI<T>(
	endpoint: string,
	options: RequestInit = {}
): Promise<T> {
	const token = await getToken();
	const headers: HeadersInit = {
		'Content-Type': 'application/json',
		...(token ? { Authorization: `Bearer ${token}` } : {}),
		...options.headers
	};

	const response = await fetch(`${API_URL}${endpoint}`, {
		...options,
		headers
	});

	if (!response.ok) {
		const error = await response.json().catch(() => ({ error: 'Unknown error' }));
		throw new Error(error.error || `HTTP ${response.status}`);
	}

	return response.json();
}

// Auth
export async function syncUser() {
	return fetchAPI<{ id: string; clerk_id: string; email: string }>('/api/auth/sync', {
		method: 'POST'
	});
}

export async function getMe() {
	return fetchAPI<{
		id: string;
		clerk_id: string;
		email: string;
		organizations: Organization[];
	}>('/api/me');
}

// Organizations
export interface Organization {
	id: string;
	name: string;
	stripe_customer_id: string | null;
	tier: string;
	created_at: string;
}

export async function listOrganizations() {
	return fetchAPI<{ organizations: Organization[] }>('/api/organizations');
}

export async function createOrganization(name: string) {
	return fetchAPI<Organization>('/api/organizations', {
		method: 'POST',
		body: JSON.stringify({ name })
	});
}

export async function getOrganization(orgId: string) {
	return fetchAPI<{
		organization: Organization;
		members: OrgMember[];
		role: string;
	}>(`/api/organizations/${orgId}`);
}

export interface OrgMember {
	org_id: string;
	user_id: string;
	role: string;
	created_at: string;
}

// Repositories
export interface Repository {
	id: string;
	org_id: string;
	owner: string;
	name: string;
	full_name: string;
	created_at: string;
}

export async function listRepositories(orgId: string) {
	return fetchAPI<{ repositories: Repository[] }>(
		`/api/organizations/${orgId}/repositories`
	);
}

export async function getRepository(orgId: string, repoId: string) {
	return fetchAPI<{ repository: Repository; analysis_count: number }>(
		`/api/organizations/${orgId}/repositories/${repoId}`
	);
}

// Analyses
export interface Analysis {
	id: string;
	repo_id: string;
	run_id: number;
	branch: string | null;
	commit_sha: string | null;
	category: string;
	confidence: number | null;
	sensitivity: string | null;
	rca: RootCauseAnalysis | null;
	text: string;
	created_at: string;
}

export interface RootCauseAnalysis {
	title: string;
	failure_type: string;
	location: { file_path: string; line_number?: number; function_name?: string } | null;
	symptom: string;
	root_cause: string;
	evidence: { type: string; content: string }[];
	remediation: string;
}

export async function listAnalyses(
	orgId: string,
	repoId: string,
	options: { limit?: number; offset?: number; category?: string } = {}
) {
	const params = new URLSearchParams();
	if (options.limit) params.set('limit', String(options.limit));
	if (options.offset) params.set('offset', String(options.offset));
	if (options.category) params.set('category', options.category);

	const query = params.toString() ? `?${params}` : '';
	return fetchAPI<{ analyses: Analysis[]; total: number; limit: number; offset: number }>(
		`/api/organizations/${orgId}/repositories/${repoId}/analyses${query}`
	);
}

export async function getAnalysis(orgId: string, analysisId: string) {
	return fetchAPI<{ analysis: Analysis; repository: Repository }>(
		`/api/organizations/${orgId}/analyses/${analysisId}`
	);
}

// Usage & Billing
export interface UsageStats {
	tier: string;
	used_this_month: number;
	limit: number;
	remaining: number;
	reset_date: string;
}

export async function getUsage(orgId: string) {
	return fetchAPI<UsageStats>(`/api/organizations/${orgId}/usage`);
}

export async function createCheckoutSession(
	orgId: string,
	tier: string,
	successUrl: string,
	cancelUrl: string
) {
	return fetchAPI<{ checkout_url: string }>('/api/billing/checkout', {
		method: 'POST',
		body: JSON.stringify({
			org_id: orgId,
			tier,
			success_url: successUrl,
			cancel_url: cancelUrl
		})
	});
}

export async function createPortalSession(orgId: string, returnUrl: string) {
	return fetchAPI<{ portal_url: string }>('/api/billing/portal', {
		method: 'POST',
		body: JSON.stringify({
			org_id: orgId,
			return_url: returnUrl
		})
	});
}
