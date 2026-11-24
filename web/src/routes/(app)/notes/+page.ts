import { api } from '#lib/api';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	return await api.listNotes(fetch, { limit: '50', sort: 'updated' });
};
