import { writable, derived, type Readable } from 'svelte/store';

export interface User {
	id: string;
	kinde_id: string;
	email: string;
	name?: string;
	created_at: string;
	organizations?: Organization[];
	kinde_org_code?: string;
	kinde_org_name?: string;
}

export interface Organization {
	id: string;
	name: string;
	stripe_customer_id: string | null;
	tier: string;
	created_at: string;
}

interface AuthState {
	user: User | null;
	isLoading: boolean;
	isAuthenticated: boolean;
	accessToken: string | null;
}

function createAuthStore() {
	const { subscribe, set, update } = writable<AuthState>({
		user: null,
		isLoading: true,
		isAuthenticated: false,
		accessToken: null
	});

	return {
		subscribe,
		setUser: (user: User | null) =>
			update((state) => ({
				...state,
				user,
				isAuthenticated: !!user,
				isLoading: false
			})),
		setAccessToken: (accessToken: string | null) =>
			update((state) => ({
				...state,
				accessToken
			})),
		setLoading: (isLoading: boolean) =>
			update((state) => ({
				...state,
				isLoading
			})),
		signOut: () =>
			set({
				user: null,
				isLoading: false,
				isAuthenticated: false,
				accessToken: null
			})
	};
}

export const auth = createAuthStore();

export const isAuthenticated: Readable<boolean> = derived(
	auth,
	($auth) => $auth.isAuthenticated
);

export const currentUser: Readable<User | null> = derived(
	auth,
	($auth) => $auth.user
);
