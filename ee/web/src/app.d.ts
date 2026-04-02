/// <reference types="@sveltejs/kit" />

declare global {
	namespace App {
		// interface Error {}
		interface Locals {
			user?: import('@kinde-oss/kinde-auth-sveltekit').KindeUser;
		}
		interface PageData {
			isAuthenticated: boolean;
			user: import('@kinde-oss/kinde-auth-sveltekit').KindeUser | null;
			accessToken: string | null;
		}
		// interface PageState {}
		// interface Platform {}
	}
}

interface ImportMetaEnv {
	readonly VITE_API_URL: string;
	readonly KINDE_CLIENT_ID: string;
	readonly KINDE_CLIENT_SECRET: string;
	readonly KINDE_ISSUER_URL: string;
	readonly KINDE_SITE_URL: string;
	readonly KINDE_POST_LOGOUT_REDIRECT_URL: string;
	readonly KINDE_POST_LOGIN_REDIRECT_URL: string;
}

interface ImportMeta {
	readonly env: ImportMetaEnv;
}

export {};
