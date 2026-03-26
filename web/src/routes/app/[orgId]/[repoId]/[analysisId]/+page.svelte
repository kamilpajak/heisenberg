<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/stores';
	import { getAnalysis, type Analysis, type Repository } from '$lib/api';
	import { ArrowLeft, FileCode, Image, Network, Terminal, Code } from 'lucide-svelte';

	let analysis: Analysis | null = null;
	let repository: Repository | null = null;
	let loading = true;

	$: orgId = $page.params.orgId;
	$: repoId = $page.params.repoId;
	$: analysisId = $page.params.analysisId;

	onMount(async () => {
		await loadData();
	});

	async function loadData() {
		loading = true;
		try {
			const result = await getAnalysis(orgId, analysisId);
			analysis = result.analysis;
			repository = result.repository;
		} catch (error) {
			console.error('Failed to load analysis:', error);
		} finally {
			loading = false;
		}
	}

	function getEvidenceIcon(type: string) {
		switch (type) {
			case 'screenshot':
				return Image;
			case 'trace':
				return Terminal;
			case 'log':
				return FileCode;
			case 'network':
				return Network;
			case 'code':
				return Code;
			default:
				return FileCode;
		}
	}

	function formatDate(date: string) {
		return new Date(date).toLocaleDateString('en-US', {
			year: 'numeric',
			month: 'long',
			day: 'numeric',
			hour: '2-digit',
			minute: '2-digit'
		});
	}
</script>

<svelte:head>
	<title>Analysis - Heisenberg</title>
</svelte:head>

<div class="space-y-6">
	<div class="flex items-center gap-4">
		<a
			href="/app/{orgId}/{repoId}"
			class="p-2 rounded-md hover:bg-accent transition-colors"
		>
			<ArrowLeft class="h-5 w-5" />
		</a>
		<div>
			<h1 class="text-2xl font-bold">Analysis Details</h1>
			<p class="text-muted-foreground">{repository?.full_name}</p>
		</div>
	</div>

	{#if loading}
		<div class="flex items-center justify-center py-12">
			<div class="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
		</div>
	{:else if analysis}
		<div class="grid gap-6">
			<!-- Header Card -->
			<div class="p-6 rounded-lg border border-border bg-card">
				<div class="flex items-start justify-between mb-4">
					<div>
						{#if analysis.rca}
							<span class="inline-block px-2 py-1 text-xs font-medium uppercase rounded bg-destructive/10 text-destructive mb-2">
								{analysis.rca.failure_type}
							</span>
							<h2 class="text-xl font-semibold">{analysis.rca.title}</h2>
						{:else}
							<h2 class="text-xl font-semibold">Run #{analysis.run_id}</h2>
						{/if}
						<p class="text-sm text-muted-foreground mt-1">{formatDate(analysis.created_at)}</p>
					</div>
					{#if analysis.confidence !== null}
						<div class="text-right">
							<div class="text-3xl font-bold">{analysis.confidence}%</div>
							<div class="text-sm text-muted-foreground">confidence</div>
						</div>
					{/if}
				</div>

				{#if analysis.rca?.location}
					<div class="flex items-center gap-2 text-sm text-muted-foreground">
						<FileCode class="h-4 w-4" />
						<span>
							{analysis.rca.location.file_path}
							{#if analysis.rca.location.line_number}
								:{analysis.rca.location.line_number}
							{/if}
						</span>
					</div>
				{/if}
			</div>

			{#if analysis.rca}
				<!-- Root Cause -->
				<div class="p-6 rounded-lg border border-border bg-card">
					<h3 class="font-semibold mb-3">Root Cause</h3>
					<p class="text-foreground">{analysis.rca.root_cause}</p>
				</div>

				<!-- Evidence -->
				{#if analysis.rca.evidence && analysis.rca.evidence.length > 0}
					<div class="p-6 rounded-lg border border-border bg-card">
						<h3 class="font-semibold mb-3">Evidence</h3>
						<div class="space-y-3">
							{#each analysis.rca.evidence as evidence}
								<div class="flex items-start gap-3">
									<svelte:component
										this={getEvidenceIcon(evidence.type)}
										class="h-5 w-5 text-muted-foreground mt-0.5"
									/>
									<div>
										<span class="text-xs font-medium uppercase text-muted-foreground">
											{evidence.type}
										</span>
										<p class="text-foreground">{evidence.content}</p>
									</div>
								</div>
							{/each}
						</div>
					</div>
				{/if}

				<!-- Remediation -->
				<div class="p-6 rounded-lg border border-border bg-card">
					<h3 class="font-semibold mb-3">Recommended Fix</h3>
					<p class="text-foreground">{analysis.rca.remediation}</p>
				</div>
			{/if}

			<!-- Raw Text -->
			<div class="p-6 rounded-lg border border-border bg-card">
				<h3 class="font-semibold mb-3">Analysis Summary</h3>
				<p class="text-foreground whitespace-pre-wrap">{analysis.text}</p>
			</div>
		</div>
	{:else}
		<div class="text-center py-12">
			<p class="text-muted-foreground">Analysis not found</p>
		</div>
	{/if}
</div>
