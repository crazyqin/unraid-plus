import { Component, type ReactNode } from 'react';
import { AlertTriangle, RefreshCw } from 'lucide-react';
import { Button } from '@/components/ui/button';
import i18n from '@/i18n';

interface Props {
  children: ReactNode;
}

interface State {
  hasError: boolean;
  error: Error | null;
}

export class ErrorBoundary extends Component<Props, State> {
  state: State = { hasError: false, error: null };

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error };
  }

  handleReload = () => {
    this.setState({ hasError: false, error: null });
  };

  render() {
    if (this.state.hasError) {
      return (
        <div className="flex min-h-[50vh] flex-col items-center justify-center gap-6 p-8 text-center">
          <div className="grid h-16 w-16 place-items-center rounded-2xl bg-destructive/10">
            <AlertTriangle className="h-8 w-8 text-destructive" />
          </div>
          <h2 className="text-display-md text-foreground">{i18n.t('errorBoundary.title')}</h2>
          <p className="max-w-md text-sm text-muted-foreground">
            {this.state.error?.message || i18n.t('errorBoundary.unknownError')}
          </p>
          <div className="flex gap-2">
            <Button variant="outline" className="rounded-lg" onClick={this.handleReload}>
              <RefreshCw className="mr-2 h-4 w-4" /> {i18n.t('errorBoundary.retry')}
            </Button>
            <Button variant="outline" className="rounded-lg" onClick={() => window.location.reload()}>
              {i18n.t('errorBoundary.refreshPage')}
            </Button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}
