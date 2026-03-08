import { fileService } from './FileService';

export const localFileService = {
    buildUserImageCachePath: async (attachmentId: string, userCode: string, isThumb: boolean = false): Promise<string> => {
        if (!window.electronAPI) return '';
        if (!userCode) {
            throw new Error('buildUserImageCachePath requires valid userCode');
        }
        const userDataPath = await window.electronAPI.fs.getPath('appData');
        const userDir = await window.electronAPI.fs.pathJoin(userDataPath, 'files', userCode);
        const fileName = isThumb ? `${attachmentId}_thumb` : attachmentId;
        return await window.electronAPI.fs.pathJoin(userDir, fileName);
    },

    // Save uploaded file bytes directly to user private directory
    saveUploadedFileToUserDir: async (file: File, userCode: string, attachmentId?: string): Promise<string> => {
        if (!window.electronAPI) return '';
        if (!userCode) {
            throw new Error('saveUploadedFileToUserDir requires valid userCode');
        }
        if (!attachmentId) {
            throw new Error('saveUploadedFileToUserDir requires attachmentId');
        }

        const userDataPath = await window.electronAPI.fs.getPath('appData');
        const userDir = await window.electronAPI.fs.pathJoin(userDataPath, 'files', userCode);
        await window.electronAPI.fs.ensureDir(userDir);
        const targetPath = await localFileService.buildUserImageCachePath(attachmentId, userCode, false);

        const exists = await window.electronAPI.fs.exists(targetPath);
        if (exists) {
            return targetPath;
        }

        const arrayBuffer = await file.arrayBuffer();
        const bytes = new Uint8Array(arrayBuffer);
        if (bytes.byteLength === 0) {
            throw new Error('Uploaded file is empty, skip local cache persistence');
        }

        await window.electronAPI.fs.saveFile(targetPath, bytes);
        const persisted = await window.electronAPI.fs.exists(targetPath);
        if (!persisted) {
            throw new Error(`Uploaded file was not persisted locally: ${targetPath}`);
        }

        return targetPath;
    },

    // Download and save file to local filesystem
    // Returns the local file path
    downloadAndSave: async (attachmentId: string, fileName: string, userCode: string): Promise<string> => {
        try {
            if (!window.electronAPI) {
                console.warn('Electron API not available');
                return '';
            }
            if (!userCode) {
                throw new Error('downloadAndSave requires valid userCode');
            }

            const userDataPath = await window.electronAPI.fs.getPath('appData');
            // Path structure: userData/files/{userCode}/{fileName}
            const userDir = await window.electronAPI.fs.pathJoin(userDataPath, 'files', userCode);
            
            // Ensure directory exists
            await window.electronAPI.fs.ensureDir(userDir);
            
            const filePath = await window.electronAPI.fs.pathJoin(userDir, fileName);

            // Check if file already exists
            const exists = await window.electronAPI.fs.exists(filePath);
            if (exists) {
                console.info('[LocalFileService] Local file already exists at:', filePath);
                return filePath;
            }

            // Download file
            const urlRes = await fileService.getFileUrl(attachmentId);
            const response = await fetch(urlRes.url);
            if (!response.ok) {
                throw new Error(`Failed to download file: ${response.statusText}`);
            }
            
            const arrayBuffer = await response.arrayBuffer();
            const bytes = new Uint8Array(arrayBuffer);
            if (bytes.byteLength === 0) {
                throw new Error(`Downloaded empty file for attachment ${attachmentId}`);
            }
            
            // Save to local file
            await window.electronAPI.fs.saveFile(filePath, bytes);

            // Verify local persistence to avoid false-positive cache_path writes
            const persisted = await window.electronAPI.fs.exists(filePath);
            if (!persisted) {
                throw new Error(`Local file was not persisted: ${filePath}`);
            }
            console.info('[LocalFileService] Local file saved at:', filePath);
            
            return filePath;
        } catch (error) {
            console.error('LocalFileService downloadAndSave error:', error);
            throw error;
        }
    },

    downloadAndSaveImage: async (attachmentId: string, userCode: string, isThumb: boolean = true): Promise<string> => {
        console.info('[LocalFileService] downloadAndSaveImage entry', {
            attachmentId,
            userCode,
            isThumb
        });
        if (!window.electronAPI) return '';
        if (!userCode) throw new Error('downloadAndSaveImage requires valid userCode');

        const urlRes = await fileService.getFileUrl(attachmentId);
        const response = await fetch(urlRes.url);
        if (!response.ok) {
            throw new Error(`Failed to download image: ${response.statusText}`);
        }

        const userDataPath = await window.electronAPI.fs.getPath('appData');
        const userDir = await window.electronAPI.fs.pathJoin(userDataPath, 'files', userCode);
        await window.electronAPI.fs.ensureDir(userDir);
        const filePath = await localFileService.buildUserImageCachePath(attachmentId, userCode, isThumb);

        const exists = await window.electronAPI.fs.exists(filePath);
        if (exists) {
            console.info('[LocalFileService] Local image already exists at:', filePath);
            return filePath;
        }

        const arrayBuffer = await response.arrayBuffer();
        const bytes = new Uint8Array(arrayBuffer);
        if (bytes.byteLength === 0) {
            throw new Error(`Downloaded empty image for attachment ${attachmentId}`);
        }
        await window.electronAPI.fs.saveFile(filePath, bytes);

        const persisted = await window.electronAPI.fs.exists(filePath);
        if (!persisted) {
            throw new Error(`Image was not persisted locally: ${filePath}`);
        }
        console.info('[LocalFileService] Local image saved at:', filePath);
        return filePath;
    },

    // Get local file content (as Blob/Url) if exists
    getLocalFileUrl: async (filePath: string): Promise<string | null> => {
        try {
            if (!window.electronAPI) return null;
            
            const exists = await window.electronAPI.fs.exists(filePath);
            if (!exists) return null;

            const buffer = await window.electronAPI.fs.readFile(filePath);
            const blob = new Blob([buffer]);
            return URL.createObjectURL(blob);
        } catch (error) {
            console.error('LocalFileService getLocalFileUrl error:', error);
            return null;
        }
    }
};
