<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/stores';
	import { getRepository, listAnalyses, type Repository, type Analysis } from '$lib/api';
	import { ArrowLeft, CheckCircle, XCircle, AlertCircle, Clock } from 'lucide-svelte';

	let repository: Repository | null = null;
	let analyses: Analysis[] = [];
	let total = 0;
	let loading = true;

	$: orgId = $page.params.orgId;
	$: repoId = $page.params.repoId;

	onMount(async () => {
		await loadData();
	});

	async function loadData() {
		loading = true;
		try {
			const [repoResult, analysesResult] = await Promise.all([
				getRepository(orgId, repoId),
				listAnalyses(orgId, repoId, { limit: 50 })
			]);
			repository = repoResult.repository;
			analyses = analysesResult.analyses || [];
			total = analysesResult.total;
		} catch (error) {
			console.error('Failed to load data:', error);
		} finally {
			loading = false;
		}
	}

	function getCategoryIcon(category: string) {
		switch (category) {
			case 'diagnosis':
				return XCircle;
			case 'no_failures':
				return CheckCircle;
			case 'not_supported':
				return AlertCircle;
			default:
				return Clock;
		}
	}

	function getCategoryColor(category: string) {
		switch (category) {
			case 'diagnosis':
				return 'text-destructive';
			case 'no_failures':
				return 'text-green-500';
			case 'not_supported':
				return 'text-yellow-500';
			default:
				return 'text-muted-foreground';
		}
	}

	function formatDate(date: string) {
		return new Date(date).toLocaleDateString('en-US', {
			year: 'numeric',
			month: 'short',
			day: 'numeric',
			hour: '2-digit',
			minute: '2-digit'
		});
	}
</script>

<svelte:head>
	<title>{repository?.full_name || 'Repository'} - Heisenberg</title>
</svelte:head>

<div class="space-y-6">
	<div class="flex items-center gap-4">
		<a
			href="/app"
			class="p-2 rounded-md hover:bg-accent transition-colors"
		>
			<ArrowLeft class="h-5 w-5" />
		</a>
		<div>
			<h1 class="text-2xl font-bold">{repository?.full_name || 'Loading...'}</h1>
			<p class="text-muted-foreground">{total} analyses</p>
		</div>
	</div>

	{#if loading}
		<div class="flex items-center justify-center py-12">
			<div class="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
		</div>
	{:else if analyses.length === 0}
		<div class="text-center py-12 border border-dashed border-border rounded-lg">
			<p class="text-muted-foreground">No analyses yet</p>
		</div>
	{:else}
		<div class="space-y-3">
			{#each analyses as analysis}
				<a
					href="/app/{orgId}/{repoId}/{analysis.id}"
					class="block p-4 rounded-lg border border-border bg-card hover:bg-accent transition-colors"
				>
					<div class="flex items-start justify-between">
						<div class="flex items-center gap-3">
							<svelte:component
								this={getCategoryIcon(analysis.category)}
								class="h-5 w-5 {getCategoryColor(analysis.category)}"
							/>
							<div>
								<div class="font-medium">
									{#if analysis.rca}
										{analysis.rca.title}
									{:else}
										Run #{analysis.run_id}
									{/if}
								</div>
								<div class="text-sm text-muted-foreground">
									{formatDate(analysis.created_at)}
									{#if analysis.branch}
										<span class="ml-2 px-2 py-0.5 bg-muted rounded text-xs">
											{analysis.branch}
										</span>
									{/if}
								</div>
							</div>
						</div>
						{#if analysis.confidence !== null}
							<div class="text-sm">
								<span class="font-medium">{analysis.confidence}%</span>
								<span class="text-muted-foreground">confidence</span>
							</div>
						{/if}
					</div>
				</a>
			{/each}
		</div>
	{/if}
</div>
