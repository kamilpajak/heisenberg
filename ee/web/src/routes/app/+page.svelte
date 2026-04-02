<script lang="ts">
	import { onMount } from 'svelte';
	import { auth } from '$stores/auth';
	import { listOrganizations, listRepositories, createOrganization, getUsage, type Organization, type Repository, type UsageStats } from '$lib/api';
	import { Plus, FolderGit2, Activity, ArrowRight } from 'lucide-svelte';

	let organizations: Organization[] = [];
	let selectedOrg: Organization | null = null;
	let repositories: Repository[] = [];
	let usage: UsageStats | null = null;
	let loading = true;
	let showCreateOrg = false;
	let newOrgName = '';

	onMount(async () => {
		await loadData();
	});

	async function loadData() {
		loading = true;
		try {
			const result = await listOrganizations();
			organizations = result.organizations || [];

			if (organizations.length > 0) {
				selectedOrg = organizations[0];
				await loadOrgData(selectedOrg.id);
			}
		} catch (error) {
			console.error('Failed to load data:', error);
		} finally {
			loading = false;
		}
	}

	async function loadOrgData(orgId: string) {
		const [repoResult, usageResult] = await Promise.all([
			listRepositories(orgId),
			getUsage(orgId)
		]);
		repositories = repoResult.repositories || [];
		usage = usageResult;
	}

	async function handleCreateOrg() {
		if (!newOrgName.trim()) return;
		try {
			const org = await createOrganization(newOrgName);
			organizations = [...organizations, org];
			selectedOrg = org;
			await loadOrgData(org.id);
			showCreateOrg = false;
			newOrgName = '';
		} catch (error) {
			console.error('Failed to create organization:', error);
		}
	}

	async function selectOrg(org: Organization) {
		selectedOrg = org;
		await loadOrgData(org.id);
	}
</script>

<svelte:head>
	<title>Dashboard - Heisenberg</title>
</svelte:head>

<div class="space-y-8">
	<div class="flex items-center justify-between">
		<div>
			<h1 class="text-3xl font-bold">Dashboard</h1>
			<p class="text-muted-foreground">Manage your test failure analyses</p>
		</div>

		{#if organizations.length > 0}
			<select
				class="px-3 py-2 rounded-md border border-input bg-background"
				value={selectedOrg?.id}
				onchange={(e) => {
					const org = organizations.find((o) => o.id === e.currentTarget.value);
					if (org) selectOrg(org);
				}}
			>
				{#each organizations as org}
					<option value={org.id}>{org.name}</option>
				{/each}
			</select>
		{/if}
	</div>

	{#if loading}
		<div class="flex items-center justify-center py-12">
			<div class="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
		</div>
	{:else if organizations.length === 0}
		<div class="text-center py-12 border border-dashed border-border rounded-lg">
			<h2 class="text-xl font-semibold mb-2">No organizations yet</h2>
			<p class="text-muted-foreground mb-4">Create your first organization to get started</p>
			<button
				onclick={() => (showCreateOrg = true)}
				class="inline-flex items-center gap-2 px-4 py-2 bg-primary text-primary-foreground rounded-md hover:bg-primary/90"
			>
				<Plus class="h-4 w-4" />
				Create Organization
			</button>
		</div>
	{:else}
		<div class="grid md:grid-cols-3 gap-6">
			<div class="p-6 rounded-lg border border-border bg-card">
				<div class="flex items-center gap-3 mb-2">
					<Activity class="h-5 w-5 text-primary" />
					<h3 class="font-semibold">Usage</h3>
				</div>
				{#if usage}
					<p class="text-3xl font-bold">
						{usage.used_this_month}
						<span class="text-lg text-muted-foreground">
							/ {usage.limit === -1 ? 'Unlimited' : usage.limit}
						</span>
					</p>
					<p class="text-sm text-muted-foreground">analyses this month</p>
				{/if}
			</div>

			<div class="p-6 rounded-lg border border-border bg-card">
				<div class="flex items-center gap-3 mb-2">
					<FolderGit2 class="h-5 w-5 text-primary" />
					<h3 class="font-semibold">Repositories</h3>
				</div>
				<p class="text-3xl font-bold">{repositories.length}</p>
				<p class="text-sm text-muted-foreground">tracked repositories</p>
			</div>

			<div class="p-6 rounded-lg border border-border bg-card">
				<div class="flex items-center gap-3 mb-2">
					<h3 class="font-semibold">Plan</h3>
				</div>
				<p class="text-3xl font-bold capitalize">{selectedOrg?.tier || 'Free'}</p>
				<a
					href="/app/settings/billing"
					class="text-sm text-primary hover:underline inline-flex items-center gap-1"
				>
					Manage plan <ArrowRight class="h-3 w-3" />
				</a>
			</div>
		</div>

		<div>
			<h2 class="text-xl font-semibold mb-4">Repositories</h2>
			{#if repositories.length === 0}
				<div class="text-center py-8 border border-dashed border-border rounded-lg">
					<p class="text-muted-foreground">
						No repositories yet. Run an analysis with the CLI to add a repository.
					</p>
				</div>
			{:else}
				<div class="space-y-2">
					{#each repositories as repo}
						<a
							href="/app/{selectedOrg?.id}/{repo.id}"
							class="flex items-center justify-between p-4 rounded-lg border border-border bg-card hover:bg-accent transition-colors"
						>
							<div class="flex items-center gap-3">
								<FolderGit2 class="h-5 w-5 text-muted-foreground" />
								<span class="font-medium">{repo.full_name}</span>
							</div>
							<ArrowRight class="h-4 w-4 text-muted-foreground" />
						</a>
					{/each}
				</div>
			{/if}
		</div>
	{/if}
</div>

{#if showCreateOrg}
	<div class="fixed inset-0 bg-background/80 backdrop-blur-sm flex items-center justify-center z-50">
		<div class="bg-card border border-border rounded-lg p-6 w-full max-w-md">
			<h2 class="text-xl font-semibold mb-4">Create Organization</h2>
			<input
				type="text"
				bind:value={newOrgName}
				placeholder="Organization name"
				class="w-full px-3 py-2 rounded-md border border-input bg-background mb-4"
			/>
			<div class="flex gap-2 justify-end">
				<button
					onclick={() => (showCreateOrg = false)}
					class="px-4 py-2 rounded-md border border-border hover:bg-accent"
				>
					Cancel
				</button>
				<button
					onclick={handleCreateOrg}
					class="px-4 py-2 rounded-md bg-primary text-primary-foreground hover:bg-primary/90"
				>
					Create
				</button>
			</div>
		</div>
	</div>
{/if}
